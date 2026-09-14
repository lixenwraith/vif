package main

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func newLogStreamProxy(target *url.URL, logger *slog.Logger) *httputil.ReverseProxy {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	// A HEAD reply looks complete to the client while LogWisp's handler is still
	// blocked on the same connection, so a pooled one stalls the next stream
	// until ResponseHeaderTimeout. One connection per stream costs nothing here.
	transport.DisableKeepAlives = true
	transport.DialContext = (&net.Dialer{
		Timeout:   2 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.ResponseHeaderTimeout = 5 * time.Second

	return &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.URL.Path = target.Path
			request.Out.URL.RawPath = target.RawPath
			request.Out.URL.RawQuery = target.RawQuery
			request.Out.Host = target.Host
		},
		Transport:     transport,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			logger.Warn("log stream unavailable", "error", err)
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			writeAPIError(w, http.StatusServiceUnavailable, "log_stream_unavailable", "The log stream is unavailable")
		},
	}
}
