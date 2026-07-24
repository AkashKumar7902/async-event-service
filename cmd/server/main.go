package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/akashkumar7902/async-event-service/internal/api"
	"github.com/akashkumar7902/async-event-service/internal/config"
	"github.com/akashkumar7902/async-event-service/internal/model"
	"github.com/akashkumar7902/async-event-service/internal/processor"
	"github.com/akashkumar7902/async-event-service/internal/stats"
)

const (
	maxRequestBytes   = int64(1 << 20) // 1 MiB
	maxUserLength     = 256
	maxTypeLength     = 128
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 15 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := run(logger); err != nil {
		logger.Error("service stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	store := stats.NewStore()
	eventProcessor, err := processor.New(processor.Options{
		WorkerCount:   cfg.WorkerCount,
		QueueCapacity: cfg.QueueCapacity,
		Store:         store,
	})
	if err != nil {
		return fmt.Errorf("create event processor: %w", err)
	}

	handler, err := api.NewHandler(api.Options{
		Processor:    eventProcessor,
		Stats:        store,
		MaxBodyBytes: maxRequestBytes,
		Validation: model.ValidationLimits{
			MaxUserLength: maxUserLength,
			MaxTypeLength: maxTypeLength,
		},
		Logger: logger,
	})
	if err != nil {
		eventProcessor.CloseInput()
		_ = eventProcessor.Wait(context.Background())
		return fmt.Errorf("create HTTP handler: %w", err)
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	logger.Info(
		"starting event service",
		"address", cfg.Addr,
		"worker_count", cfg.WorkerCount,
		"queue_capacity", cfg.QueueCapacity,
		"max_request_bytes", maxRequestBytes,
	)

	signalContext, stopSignals := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stopSignals()

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	var serveErr error
	select {
	case <-signalContext.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr = fmt.Errorf("serve HTTP: %w", err)
		}
	}

	// Restore the operating system's default signal behavior while draining.
	// A second interrupt can then force termination if graceful shutdown stalls.
	stopSignals()

	shutdownContext, cancelShutdown := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancelShutdown()

	eventProcessor.StopAccepting()

	var shutdownErr error
	if err := server.Shutdown(shutdownContext); err != nil {
		shutdownErr = fmt.Errorf("shutdown HTTP server: %w", err)
		logger.Error("HTTP shutdown did not complete cleanly", "error", err)
	}

	eventProcessor.CloseInput()

	var drainErr error
	if err := eventProcessor.Wait(shutdownContext); err != nil {
		drainErr = fmt.Errorf("drain event processor: %w", err)
		logger.Error(
			"event processor drain deadline exceeded",
			"error", err,
			"remaining_queue_depth", eventProcessor.QueueDepth(),
		)
	} else {
		logger.Info("event processor drained successfully")
	}

	return errors.Join(serveErr, shutdownErr, drainErr)
}
