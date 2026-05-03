package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"brrgate/internal/config"
	"brrgate/internal/httpserver"
	"brrgate/internal/logx"
	"brrgate/internal/patterns"
	"brrgate/internal/state"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logger := logx.New(nil, logx.ParseLevel(cfg.LogLevel))

	matcher, field, err := buildMatcher(cfg)
	if err != nil {
		logger.Error("pattern load failure", "error", err)
		os.Exit(1)
	}

	registry := state.NewRegistry()
	api := &httpserver.Webhook{
		Logger:       logger,
		Secret:       cfg.WebhookSecret,
		Matcher:      matcher,
		MatchField:   field,
		Debug:        cfg.DebugApproval,
		DebugTimeout: cfg.DebugTimeout,
		Registry:     registry,
	}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	mainChain := httpserver.WithAccessLog(logger, "webhook", mux)
	mainSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mainChain,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		logger.Info("webhook listening", "addr", cfg.ListenAddr)
		errCh <- mainSrv.ListenAndServe()
	}()

	var debugSrv *http.Server
	if cfg.DebugApproval {
		debugChain := httpserver.WithAccessLog(logger, "debug", httpserver.NewDebugHandler(registry))
		debugSrv = &http.Server{
			Addr:              cfg.DebugUIListenAddr,
			Handler:           debugChain,
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			logger.Info("debug ui listening", "addr", cfg.DebugUIListenAddr)
			errCh <- debugSrv.ListenAndServe()
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "error", err)
			stop()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := mainSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("main server shutdown", "error", err)
	}
	if debugSrv != nil {
		if err := debugSrv.Shutdown(shutdownCtx); err != nil {
			logger.Warn("debug server shutdown", "error", err)
		}
	}
}

func buildMatcher(cfg config.Config) (patterns.Matcher, string, error) {
	effective := strings.TrimSpace(cfg.MatchField)
	if cfg.PatternsFile != "" {
		loaded, err := patterns.LoadYAML(cfg.PatternsFile)
		if err != nil {
			return patterns.Matcher{}, "", err
		}
		if strings.TrimSpace(loaded.FileMatchField) != "" {
			effective = strings.TrimSpace(loaded.FileMatchField)
		}
		return loaded, effective, nil
	}
	if cfg.AutoAcceptRegex != "" {
		mm, err := patterns.FromEnvRegex(cfg.AutoAcceptRegex)
		if err != nil {
			return patterns.Matcher{}, "", err
		}
		return mm, effective, nil
	}
	return patterns.Matcher{}, effective, nil
}
