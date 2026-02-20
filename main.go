package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 9090, "HTTP server port")
	geminiBin := flag.String("gemini-bin", "gemini", "Path to the gemini CLI binary")
	geminiModel := flag.String("gemini-model", "", "Gemini model name (e.g., gemini-2.5-pro)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	// Setup logger
	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	// Check that gemini CLI binary exists
	binPath, err := exec.LookPath(*geminiBin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: Gemini CLI binary not found: %q\n\n", *geminiBin)
		fmt.Fprintf(os.Stderr, "Please make sure the Gemini CLI is installed and available in your PATH.\n")
		fmt.Fprintf(os.Stderr, "You can install it with:\n")
		fmt.Fprintf(os.Stderr, "  npm install -g @anthropic-ai/gemini-cli\n")
		fmt.Fprintf(os.Stderr, "Or specify the path explicitly:\n")
		fmt.Fprintf(os.Stderr, "  %s --gemini-bin /path/to/gemini\n", os.Args[0])
		os.Exit(1)
	}
	logger.Info("Gemini CLI found", "path", binPath)

	cfg := Config{
		GeminiBin:   binPath,
		GeminiModel: *geminiModel,
		Debug:       *debug,
	}

	manager := NewSessionManager(cfg, logger)
	handler := NewHandler(manager, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	addr := fmt.Sprintf(":%d", *port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		logger.Info("shutting down", "signal", sig.String())

		manager.CloseAll()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("server shutdown error", "error", err)
		}
	}()

	logger.Info("ACP Bridge server starting", "addr", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}
