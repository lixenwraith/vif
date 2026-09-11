package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("allocator stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string, logger *slog.Logger) error {
	cfg, err := parseConfig(args, os.Stderr)
	if err != nil {
		return err
	}
	kube, err := newKubeClient(
		cfg.KubeAPI,
		cfg.Allocator.Workload.Namespace,
		cfg.KubeCAFile,
		cfg.KubeTokenFile,
		cfg.RequestTimeout,
	)
	if err != nil {
		return err
	}
	controller := newAllocator(kube, newPodHealthProbe(3*time.Second), cfg.Allocator)

	reconcileCtx, cancelReconcile := context.WithTimeout(context.Background(), 20*time.Second)
	reconciled, err := controller.reconcile(reconcileCtx)
	cancelReconcile()
	if err != nil {
		return fmt.Errorf("startup reconciliation: %w", err)
	}
	logger.Info("reconciled Kubernetes state",
		"jobs_deleted", reconciled.JobsDeleted,
		"services_deleted", reconciled.ServicesDeleted,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	handler := newAPIServer(controller, logger)
	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// A normal create performs three Kubernetes requests before it begins
		// the readiness wait. Keep the HTTP deadline outside both bounds.
		WriteTimeout:   cfg.Allocator.ReadyTimeout + 3*cfg.RequestTimeout + 10*time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 16 << 10,
		BaseContext: func(net.Listener) context.Context {
			// Cancel in-flight allocations before Shutdown waits, so their
			// background rollback can finish inside TimeoutStopSec.
			return ctx
		},
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("allocator listening", "address", cfg.Listen, "image", cfg.Allocator.Workload.Image)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("HTTP shutdown: %w", err)
		}
		if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
