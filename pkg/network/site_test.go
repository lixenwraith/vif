package network

import "testing"

// A sign-in rides a session request as a bearer to an https site alone: over plain
// http it would cross in the clear.
func TestASessionRequestShowsTheSignInOverHTTPSAlone(t *testing.T) {
	for site, want := range map[string]string{
		"https://example.test": "Bearer tok", "HTTPS://example.test:8443": "Bearer tok", "http://example.test": "",
	} {
		req, err := siteRequest(site, 2, "td", "tok")
		if err != nil {
			t.Fatal(err)
		}
		if got := req.Header.Get("Authorization"); got != want {
			t.Errorf("%s: Authorization %q, want %q", site, got, want)
		}
		if req.URL.Path != "/vif/api/sessions" || req.Header.Get("Content-Type") != "application/json" {
			t.Errorf("%s: %s %s", site, req.URL, req.Header.Get("Content-Type"))
		}
	}
	if req, _ := siteRequest("https://example.test", 0, "", ""); req.Header.Get("Authorization") != "" {
		t.Error("no sign-in sent a header")
	}
}
