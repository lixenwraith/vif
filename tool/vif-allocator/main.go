package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
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
	logStreamURL, err := url.Parse(cfg.LogStreamURL)
	if err != nil {
		return fmt.Errorf("parse log stream URL: %w", err)
	}
	handler := newAPIServer(controller, logger, logStreamURL, cfg.Allocator)
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

	// Bound here rather than inside Serve, so a port already taken is an error this
	// returns and so readiness can be announced once the socket exists.
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("allocator listening", "address", cfg.Listen, "image", cfg.Allocator.Workload.Image)
		errCh <- server.Serve(listener)
	}()
	notifyReady(logger)

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

// notifyReady answers systemd's Type=notify, so `systemctl start` returns when the
// allocator answers rather than when it forked — every caller that restarts it then
// probes it was racing the bind. Outside systemd NOTIFY_SOCKET is unset and this
// does nothing.
func notifyReady(logger *slog.Logger) {
	name := os.Getenv("NOTIFY_SOCKET")
	if name == "" {
		return
	}
	if strings.HasPrefix(name, "@") {
		name = "\x00" + name[1:] // abstract namespace
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: name, Net: "unixgram"})
	if err == nil {
		defer conn.Close()
		_, err = conn.Write([]byte("READY=1"))
	}
	if err != nil {
		// Not fatal here: the unit's start timeout is what reports it, and failing
		// a bound listener over an unsent datagram would be the worse answer.
		logger.Warn("systemd readiness notification failed", "error", err)
	}
}
