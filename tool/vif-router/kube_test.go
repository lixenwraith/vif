package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestKubeClientReloadsTokenForEveryRequest(t *testing.T) {
	var mu sync.Mutex
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"items":[]}`)
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	client := &kubeClient{
		baseURL: baseURL, namespace: "vif", tokenFile: tokenFile, http: server.Client(),
	}
	if _, err := client.listServices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenFile, []byte("second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.listServices(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"Bearer first", "Bearer second"}
	if len(authorizations) != len(want) {
		t.Fatalf("authorizations = %v", authorizations)
	}
	for i := range want {
		if authorizations[i] != want[i] {
			t.Fatalf("authorizations = %v, want %v", authorizations, want)
		}
	}
}
