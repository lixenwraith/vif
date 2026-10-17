package service

import (
	"github.com/lixenwraith/vif/internal/vlog"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/lixenwraith/vif/internal/engine"
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
	hub := NewHub(vlog.Default())
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

	// A file of the same name beside the process must not shadow the root's copy,
	// and nothing outside the roots opens, whatever the name's shape.
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "test.vifimg"), []byte("cwd"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	if _, err := resources.Files.Open("image", "test.vifimg"); err == nil {
		t.Fatal("a working-directory file opened as an image")
	}
	for _, name := range []string{filepath.Join("..", "outside.vifimg"), image, os.DevNull} {
		if _, err := resources.Files.Open("image", name); err == nil {
			t.Fatalf("name %q opened outside the roots", name)
		}
	}
	if _, err := resources.Files.Open(filepath.Join("..", "image"), "test.vifimg"); err == nil {
		t.Fatal("invalid category accepted")
	}
	device := filepath.Join(root, "image", "device.vifimg")
	if err := os.Symlink(os.DevNull, device); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if _, err := resources.Files.Open("image", "device.vifimg"); err == nil {
		t.Fatal("a device opened as an image")
	}
}
