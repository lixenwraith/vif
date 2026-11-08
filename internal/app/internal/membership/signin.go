package membership

import (
	"encoding/json"
	"net"
	"net/url"
	"sync"

	"github.com/lixenwraith/vif/internal/app/internal/config"
	link "github.com/lixenwraith/vif/pkg/network"
)

// SignInKeeper is what a process signs in to sites with: one token a site, kept
// past the process where the host has a file for it, and shown to that site's
// front door over TLS alone (Config.SignIn). A page keeps none here: its browser
// holds the cookie the site set.
type SignInKeeper struct {
	mu     sync.Mutex
	tokens map[string]string // by the site's host
	store  config.PlaceStore
}

// NewSignInKeeper reads the sign-ins the store kept, if any; a document this build
// cannot read is left out, and replaced at the next change.
func NewSignInKeeper(store config.PlaceStore) *SignInKeeper {
	k := &SignInKeeper{tokens: make(map[string]string), store: store}
	if store == nil {
		return k
	}
	if doc, err := store.Load(); err == nil && len(doc) > 0 {
		_ = json.Unmarshal(doc, &k.tokens)
	}
	return k
}

// Token is the sign-in this process holds for the site target names, by its host;
// empty for none, which dials anonymously.
func (k *SignInKeeper) Token(target string) string {
	if k == nil {
		return ""
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.tokens[siteHost(target)]
}

// Set keeps the sign-in for site, replacing one held.
func (k *SignInKeeper) Set(site, token string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.tokens[siteHost(site)] = token
	k.storeLocked()
}

// Forget gives up the sign-in for site and returns it, for the site to revoke.
func (k *SignInKeeper) Forget(site string) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	host := siteHost(site)
	token := k.tokens[host]
	delete(k.tokens, host)
	k.storeLocked()
	return token
}

// storeLocked hands the store the sign-ins as they stand. Caller holds mu.
func (k *SignInKeeper) storeLocked() {
	if k.store == nil {
		return
	}
	if doc, err := json.Marshal(k.tokens); err == nil {
		_ = k.store.Store(doc)
	}
}

// siteHost is the host a site, a link or an address names, the one a sign-in is
// kept under: a site's front door and its API share it.
func siteHost(target string) string {
	e, err := link.ParseEndpoint(target)
	if err != nil {
		return target
	}
	if e.Scheme == link.SchemeTCP {
		if host, _, err := net.SplitHostPort(e.Addr); err == nil {
			return host
		}
		return e.Addr
	}
	if u, err := url.Parse(e.Addr); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return e.Addr
}
