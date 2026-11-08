package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// authPrefix is the route the identity service answers through the router, byte
// for byte; introspectTimeout bounds the router's one question per account ticket.
const (
	authPrefix        = "/vif/api/auth/"
	signInCookie      = "vif_sign_in"
	introspectTimeout = 2 * time.Second
	introspectReply   = 4 << 10
)

// identityClient is the router's two uses of the identity service: the forward of
// /vif/api/auth/ and the question which account a sign-in names, both on loopback.
type identityClient struct {
	forward    *httputil.ReverseProxy
	introspect *url.URL
	http       *http.Client
	log        *slog.Logger
}

// account is what introspection names for a live sign-in: the ticket's subject
// (acct:<id>) and the name the coordinator copies onto the roster.
type account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func newIdentityClient(forwardURL, introspectURL string, logger *slog.Logger) (*identityClient, error) {
	target, err := url.Parse(forwardURL)
	if err != nil {
		return nil, fmt.Errorf("identity URL: %w", err)
	}
	asked, err := url.Parse(introspectURL)
	if err != nil {
		return nil, fmt.Errorf("introspect URL: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	transport.MaxIdleConnsPerHost = 8
	transport.DialContext = (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 10 * time.Second
	c := &identityClient{introspect: asked, log: logger,
		http: &http.Client{Transport: transport, Timeout: introspectTimeout}}
	c.forward = &httputil.ReverseProxy{
		// The path stays as the player sent it; the service answers the prefix alone.
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme, r.Out.URL.Host, r.Out.Host = target.Scheme, target.Host, target.Host
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			logger.Warn("identity service unavailable", "error", err)
			w.Header().Set("Cache-Control", "no-store")
			writeAPIError(w, http.StatusServiceUnavailable, "accounts_unavailable", "The account service is unavailable")
		},
	}
	return c, nil
}

// whose asks which account the sign-in token names: the account, or nil for a
// token the service does not know, a revoked one, or a service that did not answer,
// each of which the router treats as no sign-in and mints an anonymous ticket for.
func (c *identityClient) whose(ctx context.Context, token string) *account {
	if c == nil || token == "" {
		return nil
	}
	body, _ := json.Marshal(struct {
		Token string `json:"token"`
	}{token})
	ctx, cancel := context.WithTimeout(ctx, introspectTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.introspect.String(), bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.Warn("introspection failed", "error", err)
		return nil
	}
	defer resp.Body.Close()
	var reply struct {
		Account *account `json:"account"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, introspectReply)).Decode(&reply) != nil {
		c.log.Warn("introspection answered no account", "status", resp.StatusCode)
		return nil
	}
	if reply.Account == nil || reply.Account.ID == "" || strings.ContainsAny(reply.Account.ID, " \t\r\n") {
		return nil
	}
	return reply.Account
}

// serveAccounts publishes the identity service's routes through the router and has
// both routes mint account tickets from a sign-in; nil publishes none.
func (s *apiServer) serveAccounts(c *identityClient) {
	if c == nil {
		return
	}
	s.accounts = c
	s.mux.HandleFunc(authPrefix, s.handleAuth)
	if s.ws != nil {
		s.ws.route.accounts = c
	}
}

// handleAuth forwards one request to the identity service byte for byte, with the
// player's address in the header the service believes from the router alone, so
// its budgets count the player and not the router.
func (s *apiServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	if s.clientHeader != "" {
		r.Header.Del(s.clientHeader)
		if from := s.clientAddr(r); from != nil {
			if host, _, err := net.SplitHostPort(from.String()); err == nil {
				r.Header.Set(s.clientHeader, host)
			}
		}
	}
	s.accounts.forward.ServeHTTP(w, r)
}

// signInOf is the sign-in a browser's upgrade carries, the cookie the identity
// service set on this origin; empty for none. The origin was checked before this.
func signInOf(r *http.Request) string {
	if c, err := r.Cookie(signInCookie); err == nil && c.Value != "" && !errors.Is(c.Valid(), errors.ErrUnsupported) {
		return c.Value
	}
	return ""
}
