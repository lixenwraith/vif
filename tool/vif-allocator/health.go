package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxHealthResponse = 32 << 10

type sessionHealth struct {
	Live      bool   `json:"live"`
	Ready     bool   `json:"ready"`
	Capacity  int    `json:"capacity"`
	Clock     string `json:"clock,omitempty"`
	ExpiresIn string `json:"expires_in,omitempty"`
	Guests    int    `json:"guests"`
	Phase     string `json:"phase,omitempty"`
	Tick      uint64 `json:"tick"`
	Reason    string `json:"reason,omitempty"`
}

type healthProbe interface {
	probe(context.Context, string) (sessionHealth, error)
}

type podHealthProbe struct {
	client *http.Client
	port   string
}

func newPodHealthProbe(timeout time.Duration) *podHealthProbe {
	return &podHealthProbe{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:               nil,
				DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     30 * time.Second,
			},
		},
		port: "7778",
	}
}

func (p *podHealthProbe) probe(ctx context.Context, podIP string) (sessionHealth, error) {
	if net.ParseIP(podIP) == nil {
		return sessionHealth{}, fmt.Errorf("invalid pod IP %q", podIP)
	}
	endpoint := "http://" + net.JoinHostPort(podIP, p.port) + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return sessionHealth{}, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return sessionHealth{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxHealthResponse))
		return sessionHealth{}, fmt.Errorf("health returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxHealthResponse+1))
	if err != nil {
		return sessionHealth{}, err
	}
	if len(data) > maxHealthResponse {
		return sessionHealth{}, fmt.Errorf("health response exceeds %d bytes", maxHealthResponse)
	}
	return parseHealth(data)
}

func parseHealth(data []byte) (sessionHealth, error) {
	var result sessionHealth
	seenLive := false
	seenReady := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := []string{line}
		if split := strings.Index(line, " ready="); strings.HasPrefix(line, "live=") && split > 0 {
			fields = []string{line[:split], line[split+1:]}
		}
		for _, field := range fields {
			key, value, ok := strings.Cut(field, "=")
			if !ok || key == "" {
				return sessionHealth{}, fmt.Errorf("invalid health line %q", line)
			}
			switch key {
			case "live":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return sessionHealth{}, fmt.Errorf("parse live: %w", err)
				}
				result.Live = parsed
				seenLive = true
			case "ready":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return sessionHealth{}, fmt.Errorf("parse ready: %w", err)
				}
				result.Ready = parsed
				seenReady = true
			case "capacity":
				parsed, err := strconv.Atoi(value)
				if err != nil || parsed < 0 {
					return sessionHealth{}, fmt.Errorf("parse capacity %q", value)
				}
				result.Capacity = parsed
			case "clock":
				result.Clock = value
			case "expires_in":
				result.ExpiresIn = value
			case "guests":
				parsed, err := strconv.Atoi(value)
				if err != nil || parsed < 0 {
					return sessionHealth{}, fmt.Errorf("parse guests %q", value)
				}
				result.Guests = parsed
			case "phase":
				result.Phase = value
			case "tick":
				parsed, err := strconv.ParseUint(value, 10, 64)
				if err != nil {
					return sessionHealth{}, fmt.Errorf("parse tick: %w", err)
				}
				result.Tick = parsed
			case "reason":
				result.Reason = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return sessionHealth{}, err
	}
	if !seenLive || !seenReady {
		return sessionHealth{}, fmt.Errorf("health response is missing live or ready")
	}
	return result, nil
}
