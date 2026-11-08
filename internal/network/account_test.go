package network

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/auth"
)

// accountSite stands in for a site's account service: registration keeps the
// credential a client derived, a login runs the library's exchange over it, and a
// proof issues one token, which a logout revokes.
func accountSite(t *testing.T) (*httptest.Server, *auth.ScramServer, map[string]bool) {
	t.Helper()
	scram := auth.NewScramServer()
	t.Cleanup(scram.Stop)
	tokens := map[string]bool{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vif/api/auth/register", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name       string         `json:"name"`
			Credential map[string]any `json:"credential"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, `{"error":{"code":"malformed_request","message":"bad body"}}`, http.StatusBadRequest)
			return
		}
		cred, err := auth.ImportCredential(req.Credential)
		if err != nil || cred.Username != req.Name || scram.AddCredential(cred) != nil {
			http.Error(w, `{"error":{"code":"invalid_credential","message":"the credential was refused"}}`, http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"7","name":"` + req.Name + `"}`))
	})
	mux.HandleFunc("POST /vif/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ClientFirst *auth.ClientFirstRequest `json:"client_first"`
			Proof       *auth.ClientFinalRequest `json:"proof"`
			Device      string                   `json:"device"`
			Session     string                   `json:"session"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, `{"error":{"code":"malformed_request","message":"bad body"}}`, http.StatusBadRequest)
			return
		}
		if req.ClientFirst != nil {
			challenge, err := scram.ProcessClientFirstMessage(req.ClientFirst.Username, req.ClientFirst.ClientNonce)
			if err != nil {
				http.Error(w, `{"error":{"code":"unauthorized","message":"refused"}}`, http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"challenge": challenge})
			return
		}
		final, err := scram.ProcessClientFinalMessage(req.Proof.FullNonce, req.Proof.ClientProof)
		if err != nil || req.Device == "" {
			http.Error(w, `{"error":{"code":"unauthorized","message":"refused"}}`, http.StatusUnauthorized)
			return
		}
		reply := map[string]any{"final": final, "expires_in": 2592000}
		if req.Session == "cookie" {
			http.SetCookie(w, &http.Cookie{Name: "vif_sign_in", Value: "cookie-token", Path: "/vif/"})
		} else {
			tokens["token-"+req.Device] = true
			reply["token"] = "token-" + req.Device
		}
		_ = json.NewEncoder(w).Encode(reply)
	})
	mux.HandleFunc("POST /vif/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !tokens[token] {
			http.Error(w, `{"error":{"code":"unauthorized","message":"no such sign-in"}}`, http.StatusUnauthorized)
			return
		}
		delete(tokens, token)
		w.WriteHeader(http.StatusNoContent)
	})
	site := httptest.NewServer(mux)
	t.Cleanup(site.Close)
	return site, scram, tokens
}

// TestAnAccountIsRegisteredSignedInAndOut: the credential a registration sends is
// the library's, derived here; a sign-in runs the two steps and verifies the site's
// final before trusting the token; a page asks for the cookie and gets no token; a
// wrong password ends at the proof; a logout revokes the token and a second is no error.
func TestAnAccountIsRegisteredSignedInAndOut(t *testing.T) {
	site, _, tokens := accountSite(t)
	if err := Register(site.URL, "alice", "correct horse battery", time.Second); err != nil {
		t.Fatal(err)
	}
	token, err := SignIn(site.URL, "alice", "correct horse battery", "laptop", false, time.Second)
	if err != nil || token != "token-laptop" || !tokens[token] {
		t.Fatalf("sign-in: %q %v", token, err)
	}
	if token, err := SignIn(site.URL, "alice", "correct horse battery", "page", true, time.Second); err != nil || token != "" {
		t.Fatalf("a page's sign-in: %q %v, want the cookie alone", token, err)
	}
	if _, err := SignIn(site.URL, "alice", "wrong horse battery", "laptop", false, time.Second); err == nil {
		t.Fatal("a wrong password signed in")
	}
	if _, err := SignIn(site.URL, "nobody", "correct horse battery", "laptop", false, time.Second); err == nil {
		t.Fatal("an unknown name signed in")
	}
	if err := SignOut(site.URL, token, time.Second); err != nil || tokens[token] {
		t.Fatalf("sign-out: %v, revoked %v", err, !tokens[token])
	}
	if err := SignOut(site.URL, token, time.Second); err != nil {
		t.Fatalf("a second sign-out: %v, want none", err)
	}
}
