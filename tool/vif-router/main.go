package main

import (
	"context"
	"crypto/tls"
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

	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	link "github.com/lixenwraith/vif/pkg/network"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("router stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string, logger *slog.Logger) error {
	// keygen writes the seed -ticket-key names, once, for the updater to install.
	if len(args) == 1 && args[0] == "keygen" {
		fmt.Println(network.NewKey().Seed())
		return nil
	}
	cfg, err := parseConfig(args, os.Stderr)
	if err != nil {
		return err
	}
	var ticket network.Key
	if cfg.TicketKeyFile != "" {
		text, err := os.ReadFile(cfg.TicketKeyFile)
		if err != nil {
			return fmt.Errorf("ticket key: %w", err)
		}
		if ticket, err = network.ParseKeySeed(string(text)); err != nil {
			return fmt.Errorf("ticket key %s: %w", cfg.TicketKeyFile, err)
		}
		cfg.Allocator.Workload.TicketKey = ticket.ID()
	}
	var routeTLS *tls.Config
	switch {
	case cfg.RouteCert != "":
		if routeTLS, err = link.LoadTLS(cfg.RouteCert, cfg.RouteKey); err != nil {
			return err
		}
	case cfg.RouteSelfSign:
		// A certificate for this run alone, as vif -selfsign makes one: a dial takes
		// it as it is, or pins the fingerprint the sessions API hands out with each link.
		if routeTLS, err = link.SelfSignedTLS([]string{cfg.Allocator.JoinHost}); err != nil {
			return err
		}
		cfg.Allocator.DoorFingerprint = link.Fingerprint(routeTLS)
	}
	var accounts *identityClient
	if cfg.IdentityURL != "" {
		if accounts, err = newIdentityClient(cfg.IdentityURL, cfg.IntrospectURL, logger); err != nil {
			return err
		}
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
	held := newHolds(cfg.Allocator.RouteMax)
	joins := network.NewAdmissionLimiterOf(parameter.NetworkAdmitWindow, cfg.Allocator.ClientJoins)
	// Doors runs publish through this router are reached at the front door's own
	// address, by the name the router gives them, as a pod is.
	var tunnels *network.TunnelHub
	if cfg.Allocator.Tunnels > 0 {
		tunnels = network.NewTunnelHub(cfg.Allocator.joinTarget, cfg.Allocator.Tunnels, cfg.Allocator.DoorsPerAddress)
	}
	handler := newAPIServer(controller, logger, logStreamURL, cfg.Allocator, held, joins)
	handler.routeDoors(tunnels)
	handler.admitWith(ticket)
	handler.serveAccounts(accounts)
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
	// returns and so readiness can be announced once both sockets exist.
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	routeListener, err := net.Listen("tcp", cfg.RouteListen)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("listen on %s: %w", cfg.RouteListen, err)
	}
	defer routeListener.Close()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("router listening", "address", cfg.Listen, "route", cfg.RouteListen,
			"image", cfg.Allocator.Workload.Image, "proxy_protocol", cfg.RouteProxyProtocol,
			"route_tls", routeTLS != nil, "door_fingerprint", cfg.Allocator.DoorFingerprint,
			"tickets", ticket.Valid(), "accounts", accounts != nil)
		errCh <- server.Serve(listener)
	}()
	frontDoor := newTCPRouter(controller, held, joins, logger)
	frontDoor.tunnels = tunnels
	frontDoor.ticket, frontDoor.proxyProtocol, frontDoor.tls = ticket, cfg.RouteProxyProtocol, routeTLS
	frontDoor.accounts = accounts
	go frontDoor.serve(routeListener)
	notifyReady(logger)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		_ = routeListener.Close()
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
// router answers rather than when it forked — every caller that restarts it then
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
