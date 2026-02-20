package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
)

// ---------- types ----------

// Config holds global settings for the bridge.
type Config struct {
	GeminiBin   string
	GeminiModel string
	Debug       bool
}

// MessageRecord captures a single text chunk or tool call status reported by
// the Gemini agent during a prompt turn.
type MessageRecord struct {
	Text     string          `json:"text,omitempty"`
	ToolCall *ToolCallRecord `json:"toolCall,omitempty"`
}

// ToolCallRecord represents a single tool invocation status.
type ToolCallRecord struct {
	ToolCallID string `json:"toolCallId"`
	Title      string `json:"title"`
	Status     string `json:"status"`
}

// Session wraps a running Gemini CLI subprocess and the ACP connection bound
// to it. A session supports multiple prompt turns over the same ACP session.
type Session struct {
	ID                    string
	conn                  *acp.ClientSideConnection
	acpSessionID          string
	cmd                   *exec.Cmd
	cwd                   string
	permissionCallbackURL string

	// mu protects the collected output buffer.
	mu       sync.Mutex
	messages []MessageRecord
	// promptMu serializes prompt turns for a single ACP session.
	promptMu sync.Mutex
}

// collectMessages drains the accumulated buffer and returns a copy.
func (s *Session) collectMessages() []MessageRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]MessageRecord, len(s.messages))
	copy(out, s.messages)
	s.messages = s.messages[:0]
	return out
}

// appendText appends a text chunk to the buffer.
func (s *Session) appendText(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, MessageRecord{Text: text})
}

// appendToolCall appends a tool call record to the buffer.
func (s *Session) appendToolCall(id, title, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, MessageRecord{
		ToolCall: &ToolCallRecord{
			ToolCallID: id,
			Title:      title,
			Status:     status,
		},
	})
}

// ---------- acp.Client implementation ----------

// bridgeClient implements the acp.Client interface. Each Session gets its own
// instance so that permission callbacks and output collection are correctly
// scoped.
type bridgeClient struct {
	session *Session
	logger  *slog.Logger
}

var _ acp.Client = (*bridgeClient)(nil)

// SessionUpdate is called by the ACP SDK whenever the Gemini agent sends a
// session/update notification. We collect text chunks and tool-call statuses.
func (c *bridgeClient) SessionUpdate(_ context.Context, params acp.SessionNotification) error {
	u := params.Update
	switch {
	case u.AgentMessageChunk != nil:
		content := u.AgentMessageChunk.Content
		if content.Text != nil {
			c.session.appendText(content.Text.Text)
		}
	case u.ToolCall != nil:
		c.session.appendToolCall(string(u.ToolCall.ToolCallId), u.ToolCall.Title, string(u.ToolCall.Status))
	case u.ToolCallUpdate != nil:
		status := ""
		if u.ToolCallUpdate.Status != nil {
			status = string(*u.ToolCallUpdate.Status)
		}
		c.session.appendToolCall(string(u.ToolCallUpdate.ToolCallId), "", status)
	}
	return nil
}

// ---------- permission callback ----------

// PermissionCallbackRequest is the JSON body sent to the OpenClaw agent's
// permission callback URL.
type PermissionCallbackRequest struct {
	SessionID     string                    `json:"sessionId"`
	ToolCallTitle string                    `json:"toolCallTitle"`
	Options       []PermissionOptionPayload `json:"options"`
}

// PermissionOptionPayload is a single permission option forwarded to the agent.
type PermissionOptionPayload struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// PermissionCallbackResponse is the expected response from the agent.
type PermissionCallbackResponse struct {
	SelectedOptionID string `json:"selectedOptionId,omitempty"`
	Cancelled        bool   `json:"cancelled,omitempty"`
}

// RequestPermission is called by the ACP SDK when Gemini requests permission
// for a destructive action. We forward this to the OpenClaw agent's callback
// URL and let the agent decide.
func (c *bridgeClient) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	title := ""
	if params.ToolCall.Title != nil {
		title = *params.ToolCall.Title
	}

	options := make([]PermissionOptionPayload, 0, len(params.Options))
	for _, o := range params.Options {
		options = append(options, PermissionOptionPayload{
			OptionID: string(o.OptionId),
			Name:     o.Name,
			Kind:     string(o.Kind),
		})
	}

	payload := PermissionCallbackRequest{
		SessionID:     c.session.ID,
		ToolCallTitle: title,
		Options:       options,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return acp.RequestPermissionResponse{}, fmt.Errorf("marshal permission request: %w", err)
	}

	cbResp, err := c.sendPermissionCallback(ctx, body)
	if err != nil {
		c.logger.Error("permission callback failed, denying", "error", err)
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{},
			},
		}, nil
	}

	if cbResp.Cancelled || cbResp.SelectedOptionID == "" {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{},
			},
		}, nil
	}

	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{
				OptionId: acp.PermissionOptionId(cbResp.SelectedOptionID),
			},
		},
	}, nil
}

func (c *bridgeClient) sendPermissionCallback(ctx context.Context, payload []byte) (PermissionCallbackResponse, error) {
	callbackURL := c.session.permissionCallbackURL
	if callbackURL == "" {
		return PermissionCallbackResponse{}, errors.New("permissionCallbackUrl is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(payload))
	if err != nil {
		return PermissionCallbackResponse{}, fmt.Errorf("create permission callback request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return PermissionCallbackResponse{}, fmt.Errorf("send permission callback request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return PermissionCallbackResponse{}, fmt.Errorf("read permission callback response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PermissionCallbackResponse{}, fmt.Errorf("permission callback status %d: %s", resp.StatusCode, string(respBody))
	}

	var cbResp PermissionCallbackResponse
	if err := json.Unmarshal(respBody, &cbResp); err != nil {
		return PermissionCallbackResponse{}, fmt.Errorf("unmarshal permission callback response: %w", err)
	}
	return cbResp, nil
}

func isPathWithinRoot(path string, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func isSensitiveWritePath(path string, cwd string) bool {
	cleanPath := filepath.Clean(path)
	if cwd != "" && filepath.IsAbs(cwd) && isPathWithinRoot(cleanPath, cwd) {
		return false
	}

	sensitiveRoots := []string{
		"/etc",
		"/bin",
		"/sbin",
		"/usr",
		"/var",
		"/private/etc",
		"/System",
		"/Library",
		"/Applications",
	}
	for _, root := range sensitiveRoots {
		if isPathWithinRoot(cleanPath, root) {
			return true
		}
	}

	home, err := os.UserHomeDir()
	if err == nil {
		sensitiveHomes := []string{
			filepath.Join(home, ".ssh"),
			filepath.Join(home, ".gnupg"),
			filepath.Join(home, ".aws"),
			filepath.Join(home, ".kube"),
			filepath.Join(home, ".config", "gcloud"),
		}
		for _, p := range sensitiveHomes {
			if isPathWithinRoot(cleanPath, p) {
				return true
			}
		}
	}

	// Any absolute path outside the session workspace is treated as sensitive.
	return true
}

func (c *bridgeClient) requestSensitiveWritePermission(ctx context.Context, path string) (bool, error) {
	payload := PermissionCallbackRequest{
		SessionID:     c.session.ID,
		ToolCallTitle: fmt.Sprintf("Write sensitive file path: %s", path),
		Options: []PermissionOptionPayload{
			{OptionID: "allow", Name: "Allow write", Kind: "allow"},
			{OptionID: "deny", Name: "Deny write", Kind: "deny"},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("marshal sensitive write permission request: %w", err)
	}
	cbResp, err := c.sendPermissionCallback(ctx, body)
	if err != nil {
		return false, err
	}
	if cbResp.Cancelled {
		return false, nil
	}
	return cbResp.SelectedOptionID == "allow", nil
}

// ---------- file system callbacks ----------

func (c *bridgeClient) ReadTextFile(_ context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	if !filepath.IsAbs(params.Path) {
		return acp.ReadTextFileResponse{}, fmt.Errorf("path must be absolute: %s", params.Path)
	}
	b, err := os.ReadFile(params.Path)
	if err != nil {
		return acp.ReadTextFileResponse{}, fmt.Errorf("read %s: %w", params.Path, err)
	}
	content := string(b)
	if params.Line != nil || params.Limit != nil {
		lines := strings.Split(content, "\n")
		start := 0
		if params.Line != nil && *params.Line > 0 {
			start = min(max(*params.Line-1, 0), len(lines))
		}
		end := len(lines)
		if params.Limit != nil && *params.Limit > 0 {
			if start+*params.Limit < end {
				end = start + *params.Limit
			}
		}
		content = strings.Join(lines[start:end], "\n")
	}
	c.logger.Debug("ReadTextFile", "path", params.Path, "bytes", len(content))
	return acp.ReadTextFileResponse{Content: content}, nil
}

func (c *bridgeClient) WriteTextFile(ctx context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if !filepath.IsAbs(params.Path) {
		return acp.WriteTextFileResponse{}, fmt.Errorf("path must be absolute: %s", params.Path)
	}

	if isSensitiveWritePath(params.Path, c.session.cwd) {
		allowed, err := c.requestSensitiveWritePermission(ctx, params.Path)
		if err != nil {
			return acp.WriteTextFileResponse{}, fmt.Errorf("request sensitive write permission failed: %w", err)
		}
		if !allowed {
			return acp.WriteTextFileResponse{}, fmt.Errorf("write denied by user for sensitive path: %s", params.Path)
		}
	}

	dir := filepath.Dir(params.Path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return acp.WriteTextFileResponse{}, fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(params.Path, []byte(params.Content), 0o644); err != nil {
		return acp.WriteTextFileResponse{}, fmt.Errorf("write %s: %w", params.Path, err)
	}
	c.logger.Debug("WriteTextFile", "path", params.Path, "bytes", len(params.Content))
	return acp.WriteTextFileResponse{}, nil
}

// ---------- terminal stubs ----------

func (c *bridgeClient) CreateTerminal(_ context.Context, _ acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{TerminalId: "stub-term"}, nil
}

func (c *bridgeClient) TerminalOutput(_ context.Context, _ acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{Output: "", Truncated: false}, nil
}

func (c *bridgeClient) ReleaseTerminal(_ context.Context, _ acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, nil
}

func (c *bridgeClient) WaitForTerminalExit(_ context.Context, _ acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, nil
}

func (c *bridgeClient) KillTerminalCommand(_ context.Context, _ acp.KillTerminalCommandRequest) (acp.KillTerminalCommandResponse, error) {
	return acp.KillTerminalCommandResponse{}, nil
}

// ---------- session manager ----------

// SessionManager manages the lifecycle of multiple ACP sessions.
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	config   Config
	logger   *slog.Logger
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(cfg Config, logger *slog.Logger) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
		config:   cfg,
		logger:   logger,
	}
}

// CreateSession spawns a Gemini CLI subprocess, performs the ACP handshake
// (Initialize + NewSession), and returns a Session ready for prompt calls.
func (m *SessionManager) CreateSession(ctx context.Context, cwd string, permissionCallbackURL string) (*Session, error) {
	args := []string{"--experimental-acp"}
	if m.config.GeminiModel != "" {
		args = append(args, "--model", m.config.GeminiModel)
	}
	if m.config.Debug {
		args = append(args, "--debug")
	}

	cmd := exec.CommandContext(ctx, m.config.GeminiBin, args...)
	cmd.Stderr = os.Stderr
	cmd.Dir = cwd

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start gemini cli: %w", err)
	}

	sessionID := fmt.Sprintf("sess-%d", cmd.Process.Pid)
	sess := &Session{
		ID:                    sessionID,
		cmd:                   cmd,
		cwd:                   cwd,
		permissionCallbackURL: permissionCallbackURL,
	}

	client := &bridgeClient{session: sess, logger: m.logger}
	conn := acp.NewClientSideConnection(client, stdin, stdout)
	conn.SetLogger(m.logger)
	sess.conn = conn

	// ACP Initialize
	initResp, err := conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapability{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
	})
	if err != nil {
		killAndWaitProcess(cmd)
		return nil, fmt.Errorf("ACP initialize failed: %w", err)
	}
	m.logger.Info("ACP initialized", "protocolVersion", initResp.ProtocolVersion)

	// ACP NewSession
	newSessResp, err := conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		killAndWaitProcess(cmd)
		return nil, fmt.Errorf("ACP NewSession failed: %w", err)
	}
	sess.acpSessionID = string(newSessResp.SessionId)
	m.logger.Info("ACP session created", "acpSessionId", newSessResp.SessionId)

	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()

	return sess, nil
}

// Prompt sends a prompt to an existing session and blocks until the Gemini
// agent finishes.
func (m *SessionManager) Prompt(ctx context.Context, sessionID string, prompt string) (*PromptResult, error) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	sess.promptMu.Lock()
	defer sess.promptMu.Unlock()

	resp, err := sess.conn.Prompt(ctx, acp.PromptRequest{
		SessionId: acp.SessionId(sess.acpSessionID),
		Prompt:    []acp.ContentBlock{acp.TextBlock(prompt)},
	})
	if err != nil {
		return nil, fmt.Errorf("ACP prompt failed: %w", err)
	}

	collected := sess.collectMessages()

	// Merge all text chunks into one string.
	var textParts []string
	var toolCalls []ToolCallRecord
	for _, msg := range collected {
		if msg.Text != "" {
			textParts = append(textParts, msg.Text)
		}
		if msg.ToolCall != nil {
			toolCalls = append(toolCalls, *msg.ToolCall)
		}
	}

	stopReason := string(resp.StopReason)

	return &PromptResult{
		Text:       strings.Join(textParts, ""),
		ToolCalls:  toolCalls,
		StopReason: stopReason,
	}, nil
}

func killAndWaitProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

// CloseSession kills the Gemini CLI subprocess and removes the session.
func (m *SessionManager) CloseSession(sessionID string) error {
	m.mu.Lock()
	sess, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if sess.cmd.Process != nil {
		_ = sess.cmd.Process.Kill()
		_ = sess.cmd.Wait()
	}

	m.logger.Info("session closed", "sessionId", sessionID)
	return nil
}

// CloseAll kills all running sessions. Called during graceful shutdown.
func (m *SessionManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, sess := range m.sessions {
		if sess.cmd.Process != nil {
			_ = sess.cmd.Process.Kill()
			_ = sess.cmd.Wait()
		}
		m.logger.Info("session closed (shutdown)", "sessionId", id)
	}
	m.sessions = make(map[string]*Session)
}

// PromptResult is the structured result returned after a prompt turn.
type PromptResult struct {
	Text       string           `json:"text"`
	ToolCalls  []ToolCallRecord `json:"toolCalls"`
	StopReason string           `json:"stopReason"`
}
