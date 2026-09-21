package main

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func proxyTestServer(t *testing.T, target string) *apiServer {
	t.Helper()
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	return newAPIServer(
		&fakeSessionAllocator{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		parsed,
		allocatorConfig{},
	)
}

func TestLogProxyPreservesResponse(t *testing.T) {
	wantBody := "event: record\ndata: {\"fields\":{\"msg\":\"exact  spacing\"}}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/stream" || r.URL.RawQuery != "" {
			t.Errorf("upstream request = %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.Header().Set("X-LogWisp-Test", "preserved")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, wantBody)
	}))
	defer upstream.Close()

	request := httptest.NewRequest(http.MethodGet, "/vif/api/logs?ignored=yes", nil)
	response := httptest.NewRecorder()
	proxyTestServer(t, upstream.URL+"/stream").ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Body.String(); got != wantBody {
		t.Fatalf("body changed:\n got %q\nwant %q", got, wantBody)
	}
	for name, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"X-Accel-Buffering": "no",
		"X-LogWisp-Test":    "preserved",
	} {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestLogProxyRejectsOtherMethods(t *testing.T) {
	server := proxyTestServer(t, "http://127.0.0.1:1/stream")
	post := httptest.NewRecorder()
	server.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/vif/api/logs", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST status = %d, Allow = %q", post.Code, post.Header().Get("Allow"))
	}
}

func TestLogProxyFlushesAndPropagatesCancellation(t *testing.T) {
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamCanceled)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: connected\ndata: first\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()

	proxy := httptest.NewServer(proxyTestServer(t, upstream.URL+"/stream"))
	defer proxy.Close()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.URL+"/vif/api/logs", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "event: connected\n" {
		t.Fatalf("first line = %q, err = %v", line, err)
	}

	cancel()
	_ = response.Body.Close()
	select {
	case <-upstreamCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request was not canceled")
	}
}

func TestLogProxyReturnsStableUnavailableResponse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := "http://" + listener.Addr().String() + "/stream"
	_ = listener.Close()

	response := httptest.NewRecorder()
	proxyTestServer(t, target).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/vif/api/logs", nil),
	)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.TrimSpace(response.Body.String()) != `{"error":{"code":"log_stream_unavailable","message":"The log stream is unavailable"}}` {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestLogProxyRepeatedReconnectsReleaseUpstream(t *testing.T) {
	var active atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: ready\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	proxy := httptest.NewServer(proxyTestServer(t, upstream.URL+"/stream"))
	defer proxy.Close()

	for range 5 {
		ctx, cancel := context.WithCancel(context.Background())
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.URL+"/vif/api/logs", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if _, err := bufio.NewReader(response.Body).ReadString('\n'); err != nil {
			cancel()
			_ = response.Body.Close()
			t.Fatal(err)
		}
		cancel()
		_ = response.Body.Close()

		deadline := time.Now().Add(2 * time.Second)
		for active.Load() != 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := active.Load(); got != 0 {
			t.Fatalf("active upstream requests after reconnect = %d", got)
		}
	}
}

// A HEAD is a route probe. Upstream refuses one on the stream path, because the
// client it would register never reads its discarded body, so the allocator
// answers it without opening a stream at all.
func TestLogProxyAnswersHeadWithoutOpeningAStream(t *testing.T) {
	var upstreamRequests atomic.Int64
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamRequests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: connected\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer upstream.Close()
	defer close(release)
	proxy := httptest.NewServer(proxyTestServer(t, upstream.URL+"/stream"))
	defer proxy.Close()

	head, err := http.Head(proxy.URL + "/vif/api/logs")
	if err != nil {
		t.Fatal(err)
	}
	_ = head.Body.Close()
	if head.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d", head.StatusCode)
	}
	if got := head.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("HEAD Content-Type = %q", got)
	}
	if got := upstreamRequests.Load(); got != 0 {
		t.Fatalf("upstream requests from HEAD = %d, want 0", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.URL+"/vif/api/logs", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want a stream", response.StatusCode)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimRight(line, "\n") != "event: connected" {
		t.Fatalf("first line = %q", line)
	}
}
