package membership

import (
	"testing"

	"github.com/lixenwraith/vif/internal/app/internal/config"
)

// TestASignInIsKeptBySiteAndShownToItsDoors: a sign-in is kept under the site's
// host, which the site's front door, its browser route and its origin share, past
// the keeper's life; a forgotten one is handed back once, for the site to revoke.
func TestASignInIsKeptBySiteAndShownToItsDoors(t *testing.T) {
	store := &memoryPlaces{}
	k := NewSignInKeeper(store)
	k.Set("https://example.com", "tok")
	for _, target := range []string{"vifs://example.com:7777/0123456789abcdef", "wss://example.com/vif/ws/0123456789abcdef", "example.com:7777", "https://example.com"} {
		if got := k.Token(target); got != "tok" {
			t.Fatalf("%s: %q, want the site's sign-in", target, got)
		}
	}
	if got := k.Token("vifs://other.example:7777/abc"); got != "" {
		t.Fatalf("another site was shown %q", got)
	}
	if got := NewSignInKeeper(store).Token("https://example.com"); got != "tok" {
		t.Fatal("the sign-in did not outlive the keeper")
	}
	if got := k.Forget("https://example.com"); got != "tok" || k.Token("https://example.com") != "" || k.Forget("https://example.com") != "" {
		t.Fatal("a forgotten sign-in is handed back once")
	}
	var none *SignInKeeper
	if none.Token("https://example.com") != "" {
		t.Fatal("a run with no keeper dials with a sign-in")
	}
}

// TestJoiningKeepsTheSecureScheme: a vifs:// link joins under TLS, so the address a
// run dials keeps the scheme that says so; a plain link and a wss one are as given.
func TestJoiningKeepsTheSecureScheme(t *testing.T) {
	var base config.Membership
	for target, want := range map[string]string{
		"vifs://example.com:7777/0123456789abcdef":  "vifs://example.com:7777",
		"vif://example.com:7777/0123456789abcdef":   "example.com:7777",
		"wss://example.com/vif/ws/0123456789abcdef": "wss://example.com/vif/ws/0123456789abcdef",
	} {
		s, err := Joining(base, target)
		if err != nil || s.JoinAddress != want {
			t.Fatalf("%s: %q %v, want %q", target, s.JoinAddress, err, want)
		}
	}
}
