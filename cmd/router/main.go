// Package main is the entry point for the Local Model Router.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kalle/local-model-router/internal/admin"
	"github.com/kalle/local-model-router/internal/classifier"
	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/keystore"
	"github.com/kalle/local-model-router/internal/metrics"
	"github.com/kalle/local-model-router/internal/ollama"
	"github.com/kalle/local-model-router/internal/server"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Version information (set via ldflags during build)
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	// Show version and exit if requested
	if *showVersion {
		fmt.Printf("Local Model Router %s\n", version)
		fmt.Printf("  Commit: %s\n", commit)
		fmt.Printf("  Built:  %s\n", buildDate)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	// Setup logging
	setupLogging(cfg.Logging)

	log.Info().
		Str("version", version).
		Str("config", *configPath).
		Msg("Starting Local Model Router")

	// Log configuration (without sensitive values)
	log.Debug().
		Str("server_address", cfg.Server.Address()).
		Str("ollama_endpoint", cfg.Ollama.Endpoint).
		Bool("auth_enabled", cfg.Auth.Enabled).
		Bool("admin_enabled", cfg.Admin.Enabled).
		Bool("metrics_enabled", cfg.Metrics.Enabled).
		Msg("Configuration loaded")

	// Create root context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// --- Initialize components ---

	// Ollama client and model cache
	ollamaClient := ollama.NewClient(cfg.Ollama.Endpoint, cfg.Ollama.Timeout)
	modelCache := ollama.NewModelCache(ollamaClient, cfg.Ollama.RefreshInterval)
	if err := modelCache.Start(ctx); err != nil {
		log.Warn().Err(err).Msg("Model cache failed initial refresh; continuing")
	}

	// Key store (SQLite). Ensure the parent directory exists.
	if dir := filepath.Dir(cfg.Keystore.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal().Err(err).Str("path", dir).Msg("Failed to create keystore directory")
		}
	}
	keyStore, err := keystore.NewSQLiteStore(cfg.Keystore.Path)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize key store")
	}

	// Classifier and router
	classifierInstance, err := classifier.NewClassifier(buildClassifierConfig(cfg.Routing))
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize classifier")
	}
	modelRouter := classifier.NewRouter(classifierInstance, modelCache, cfg.Routing)

	// Reflect the force-routing configuration in metrics.
	metrics.SetForceRouting(cfg.Routing.ForceRouting)

	// Admin handler (only mounted if enabled)
	var adminHandler *admin.Handler
	if cfg.Admin.Enabled {
		adminHandler = admin.NewHandler(&cfg.Admin, keyStore, modelCache, ollamaClient, modelRouter)
	}

	// HTTP server
	srv := server.NewServer(cfg, ollamaClient, modelCache, keyStore, classifierInstance, modelRouter, adminHandler)

	log.Info().
		Str("address", cfg.Server.Address()).
		Msg("Server starting")

	// Start HTTP server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			serverErr <- err
		}
	}()

	log.Info().
		Bool("auth_enabled", cfg.Auth.Enabled).
		Bool("admin_enabled", cfg.Admin.Enabled).
		Bool("metrics_enabled", cfg.Metrics.Enabled).
		Int("models", modelCache.Count()).
		Msg("Server ready")

	// Wait for shutdown signal or fatal server error
	select {
	case sig := <-sigChan:
		log.Info().Str("signal", sig.String()).Msg("Shutdown signal received")
	case err := <-serverErr:
		log.Error().Err(err).Msg("HTTP server failed")
	}

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Graceful shutdown of components
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
	}
	modelCache.Stop()
	if err := keyStore.Close(); err != nil {
		log.Error().Err(err).Msg("Key store close error")
	}
	cancel()

	log.Info().Msg("Server stopped")
}

// buildClassifierConfig converts the routing configuration into the
// classifier's configuration format.
func buildClassifierConfig(routing config.RoutingConfig) classifier.Config {
	cc := classifier.Config{
		Capabilities: make(map[string]classifier.CapabilityConfig, len(routing.Capabilities)),
	}
	for name, cap := range routing.Capabilities {
		cc.Capabilities[name] = classifier.CapabilityConfig{
			Keywords:       cap.Keywords,
			Patterns:       cap.Patterns,
			MaxInputTokens: cap.MaxInputTokens,
		}
	}
	for _, rule := range routing.Rules {
		cc.Rules = append(cc.Rules, classifier.RuleConfig{
			Name:     rule.Name,
			Pattern:  rule.Pattern,
			Model:    rule.Model,
			Priority: rule.Priority,
		})
	}
	return cc
}

// setupLogging configures the global logger based on configuration.
func setupLogging(cfg config.LoggingConfig) {
	// Set log level
	switch cfg.Level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Set output format
	if cfg.Format == "text" {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		})
	} else {
		// JSON format is the default
		zerolog.TimeFieldFormat = time.RFC3339
	}
}
