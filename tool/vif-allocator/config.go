package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	dnsLabelPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	mapSizePattern  = regexp.MustCompile(`^[1-9][0-9]*x[1-9][0-9]*$`)
)

type runtimeConfig struct {
	Listen         string
	KubeAPI        string
	KubeCAFile     string
	KubeTokenFile  string
	RequestTimeout time.Duration
	Allocator      allocatorConfig
}

func parseConfig(args []string, output io.Writer) (runtimeConfig, error) {
	var cfg runtimeConfig
	var firstJoin string
	var empty string
	var drain string

	set := flag.NewFlagSet("vif-allocator", flag.ContinueOnError)
	set.SetOutput(output)
	set.StringVar(&cfg.Listen, "listen", ":9080", "allocator HTTP listen address")
	set.StringVar(&cfg.KubeAPI, "kube-api", "https://127.0.0.1:6443", "Kubernetes API URL")
	set.StringVar(&cfg.KubeCAFile, "kube-ca", "/etc/vif-allocator/server-ca.crt", "Kubernetes CA certificate")
	set.StringVar(&cfg.KubeTokenFile, "kube-token", "/etc/vif-allocator/token", "rotated ServiceAccount token file")
	set.DurationVar(&cfg.RequestTimeout, "kube-timeout", 10*time.Second, "timeout for one Kubernetes API request")
	set.StringVar(&cfg.Allocator.Workload.Namespace, "namespace", "vif", "Kubernetes namespace")
	set.StringVar(&cfg.Allocator.Workload.Image, "image", "", "session image reference (required)")
	set.IntVar(&cfg.Allocator.Workload.Players, "players", 4, "session guest ceiling")
	set.StringVar(&cfg.Allocator.Workload.MapSize, "map-size", "120x40", "session map size")
	set.StringVar(&firstJoin, "first-join", "90s", "first guest deadline")
	set.StringVar(&empty, "empty", "90s", "empty roster grace")
	set.StringVar(&drain, "drain", "20s", "termination drain deadline")
	set.StringVar(&cfg.Allocator.JoinHost, "join-host", "", "public raw-TCP host returned to players (required)")
	set.StringVar(&cfg.Allocator.PageBase, "page-base", "", "absolute session page base URL (required)")
	set.IntVar(&cfg.Allocator.PortFirst, "port-first", 31700, "first allocatable NodePort")
	set.IntVar(&cfg.Allocator.PortLast, "port-last", 31709, "last allocatable NodePort")
	set.DurationVar(&cfg.Allocator.ReadyTimeout, "ready-timeout", 75*time.Second, "session readiness deadline")
	set.DurationVar(&cfg.Allocator.PollInterval, "poll-interval", time.Second, "session readiness polling interval")
	cfg.Allocator.CleanupTimeout = 10 * time.Second

	if err := set.Parse(args); err != nil {
		return runtimeConfig{}, err
	}
	if set.NArg() != 0 {
		return runtimeConfig{}, fmt.Errorf("unexpected argument %q", set.Arg(0))
	}
	cfg.Allocator.Workload.FirstJoin = firstJoin
	cfg.Allocator.Workload.Empty = empty
	cfg.Allocator.Workload.Drain = drain
	if err := validateConfig(cfg); err != nil {
		return runtimeConfig{}, err
	}
	return cfg, nil
}

func validateConfig(cfg runtimeConfig) error {
	if _, _, err := net.SplitHostPort(cfg.Listen); err != nil {
		return fmt.Errorf("invalid -listen: %w", err)
	}
	if !dnsLabelPattern.MatchString(cfg.Allocator.Workload.Namespace) {
		return fmt.Errorf("invalid -namespace %q", cfg.Allocator.Workload.Namespace)
	}
	if cfg.Allocator.Workload.Image == "" {
		return fmt.Errorf("-image is required")
	}
	if strings.ContainsAny(cfg.Allocator.Workload.Image, "<>\t\r\n ") {
		return fmt.Errorf("invalid -image %q", cfg.Allocator.Workload.Image)
	}
	if cfg.Allocator.Workload.Players < 1 || cfg.Allocator.Workload.Players > 16 {
		return fmt.Errorf("-players must be between 1 and 16")
	}
	if !mapSizePattern.MatchString(cfg.Allocator.Workload.MapSize) {
		return fmt.Errorf("invalid -map-size %q", cfg.Allocator.Workload.MapSize)
	}
	for name, value := range map[string]string{
		"-first-join": cfg.Allocator.Workload.FirstJoin,
		"-empty":      cfg.Allocator.Workload.Empty,
		"-drain":      cfg.Allocator.Workload.Drain,
	} {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return fmt.Errorf("%s must be a positive duration", name)
		}
	}
	if cfg.Allocator.JoinHost == "" || strings.Contains(cfg.Allocator.JoinHost, "/") {
		return fmt.Errorf("-join-host must be a host without a scheme or port")
	}
	if net.ParseIP(cfg.Allocator.JoinHost) == nil && strings.Contains(cfg.Allocator.JoinHost, ":") {
		return fmt.Errorf("-join-host must not include a port")
	}
	pageBase, err := url.Parse(cfg.Allocator.PageBase)
	if err != nil || pageBase.Host == "" || (pageBase.Scheme != "https" && pageBase.Scheme != "http") {
		return fmt.Errorf("-page-base must be an absolute http or https URL")
	}
	if cfg.Allocator.PortFirst < 1 || cfg.Allocator.PortLast > 65535 || cfg.Allocator.PortFirst > cfg.Allocator.PortLast {
		return fmt.Errorf("invalid NodePort range %d-%d", cfg.Allocator.PortFirst, cfg.Allocator.PortLast)
	}
	if cfg.RequestTimeout <= 0 || cfg.Allocator.ReadyTimeout <= 0 || cfg.Allocator.PollInterval <= 0 {
		return fmt.Errorf("timeouts and poll interval must be positive")
	}
	if cfg.KubeCAFile == "" || cfg.KubeTokenFile == "" {
		return fmt.Errorf("Kubernetes CA and token files are required")
	}
	return nil
}
