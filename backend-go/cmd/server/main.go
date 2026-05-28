package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/api"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
)

func main() {
	// 1. Load config
	cfg := config.Load()

	// 2. Configure zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// 3. Create hub and start it
	hub := api.NewHub()
	api.SetGlobalHub(hub)
	go hub.Run(context.Background())

	// 4. Create server and get router
	srv := api.NewServer(cfg)
	router := srv.Router()

	// 5. Start HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.BackendHost, cfg.BackendPort)
	log.Info().Str("addr", addr).Str("llm_provider", string(cfg.LLMProvider)).Msg("NoVo-Transcriber backend starting")

	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // streaming
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info().Msg("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()

	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("Server error")
	}
}
