package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var (
	dnsLabelPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	mapSizePattern  = regexp.MustCompile(`^[1-9][0-9]*x[1-9][0-9]*$`)
)

// sessionLogLevels are the game's -lv names, most verbose first. A request may
// select from -log-level-min onward, which defaults past trace: the fleet's log rate
// and its tmpfs are shared, and an anonymous caller must not be able to raise one
// session's output at the cost of every other session's records.
var sessionLogLevels = []string{"trace", "debug", "info", "warn", "error"}

type runtimeConfig struct {
	Listen         string
	KubeAPI        string
	KubeCAFile     string
	KubeTokenFile  string
	LogStreamURL   string
	RequestTimeout time.Duration
	Allocator      allocatorConfig
}

func parseConfig(args []string, output io.Writer) (runtimeConfig, error) {
	var cfg runtimeConfig
	var firstJoin string
	var empty string
	var drain string
	var logLevelMin string

	set := flag.NewFlagSet("vif-allocator", flag.ContinueOnError)
	set.SetOutput(output)
	set.StringVar(&cfg.Listen, "listen", ":9080", "allocator HTTP listen address")
	set.StringVar(&cfg.KubeAPI, "kube-api", "https://127.0.0.1:6443", "Kubernetes API URL")
	set.StringVar(&cfg.KubeCAFile, "kube-ca", "/etc/vif-allocator/server-ca.crt", "Kubernetes CA certificate")
	set.StringVar(&cfg.KubeTokenFile, "kube-token", "/etc/vif-allocator/token", "rotated ServiceAccount token file")
	set.StringVar(&cfg.LogStreamURL, "log-stream-url", "", "loopback LogWisp SSE URL (required)")
	set.DurationVar(&cfg.RequestTimeout, "kube-timeout", 10*time.Second, "timeout for one Kubernetes API request")
	set.StringVar(&cfg.Allocator.Workload.Namespace, "namespace", "vif", "Kubernetes namespace")
	set.StringVar(&cfg.Allocator.Workload.Image, "image", "", "session image reference (required)")
	set.IntVar(&cfg.Allocator.Workload.Players, "players", 4, "default session guest ceiling")
	set.IntVar(&cfg.Allocator.PlayersMax, "players-max", 0,
		"highest guest ceiling a request may select; 0 keeps -players as the only one")
	set.StringVar(&cfg.Allocator.Workload.LogLevel, "log-level", "info", "default session log level")
	set.StringVar(&logLevelMin, "log-level-min", "debug",
		"most verbose session log level a request may select")
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
	if cfg.Allocator.PlayersMax == 0 {
		cfg.Allocator.PlayersMax = cfg.Allocator.Workload.Players
	}
	floor := slices.Index(sessionLogLevels, logLevelMin)
	if floor < 0 {
		return runtimeConfig{}, fmt.Errorf("-log-level-min must be one of %s",
			strings.Join(sessionLogLevels, ", "))
	}
	cfg.Allocator.LogLevels = sessionLogLevels[floor:]
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
	if cfg.Allocator.PlayersMax < cfg.Allocator.Workload.Players || cfg.Allocator.PlayersMax > 16 {
		return fmt.Errorf("-players-max must be between -players (%d) and 16",
			cfg.Allocator.Workload.Players)
	}
	if !slices.Contains(cfg.Allocator.LogLevels, cfg.Allocator.Workload.LogLevel) {
		return fmt.Errorf("-log-level %q is more verbose than -log-level-min allows",
			cfg.Allocator.Workload.LogLevel)
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
	if err := validateLogStreamURL(cfg.LogStreamURL); err != nil {
		return err
	}
	return nil
}

func validateLogStreamURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("-log-stream-url is required")
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "http" || target.Host == "" {
		return fmt.Errorf("-log-stream-url must be an absolute http URL")
	}
	if target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return fmt.Errorf("-log-stream-url must not contain credentials, a query, or a fragment")
	}
	if target.EscapedPath() != "/stream" {
		return fmt.Errorf("-log-stream-url path must be /stream")
	}
	host := net.ParseIP(target.Hostname())
	if host == nil || !host.IsLoopback() {
		return fmt.Errorf("-log-stream-url host must be a loopback IP address")
	}
	port := target.Port()
	if port == "" {
		return fmt.Errorf("-log-stream-url must include a port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("invalid -log-stream-url port %q", port)
	}
	return nil
}
