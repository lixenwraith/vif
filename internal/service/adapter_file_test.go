package service

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
)

func TestFileServiceContributesCategorizedOpenCapability(t *testing.T) {
	root := t.TempDir()
	image := filepath.Join(root, "image", "backdrops", "test.vifimg")
	if err := os.MkdirAll(filepath.Dir(image), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(image, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := NewFileService(FileSource{Roots: []string{root}})
	hub := NewHub()
	if err := hub.Register(svc); err != nil {
		t.Fatal(err)
	}
	if err := hub.InitAll(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.StopAll)
	resources := &engine.Resource{}
	hub.BindResources(resources)

	f, err := resources.Files.Open("image", filepath.Join("backdrops", "test.vifimg"))
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(f)
	closeErr := f.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read = %v, close = %v", readErr, closeErr)
	}
	if string(got) != "fixture" {
		t.Fatalf("body = %q, want fixture", got)
	}

	explicitDir := t.TempDir()
	explicit := filepath.Join(explicitDir, "relative.vifimg")
	if err := os.WriteFile(explicit, []byte("explicit"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(explicitDir)
	f, err = resources.Files.Open("image", "relative.vifimg")
	if err != nil {
		t.Fatal(err)
	}
	got, readErr = io.ReadAll(f)
	closeErr = f.Close()
	if readErr != nil || closeErr != nil || string(got) != "explicit" {
		t.Fatalf("explicit body = %q, read = %v, close = %v", got, readErr, closeErr)
	}

	if _, err := resources.Files.Open("image", filepath.Join("..", "outside.vifimg")); err == nil {
		t.Fatal("logical name escaped its category")
	}
	if _, err := resources.Files.Open(filepath.Join("..", "image"), "test.vifimg"); err == nil {
		t.Fatal("invalid category accepted")
	}
}
