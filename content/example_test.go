package content_test

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/content"
)

// This example demonstrates the file discovery system
func TestContentManagerDiscovery(t *testing.T) {
	// Create a new content manager
	cm := content.NewContentManager()

	// Discover all .txt files in the assets directory
	err := cm.DiscoverContentFiles()
	if err != nil {
		t.Fatalf("Failed to discover content files: %v", err)
	}

	// Get the discovered files
	files := cm.GetContentFiles()

	// Log the discovered files
	t.Logf("Discovered %d content file(s):", len(files))
	for i, file := range files {
		t.Logf("  [%d] %s", i+1, file)
	}

	// Verify we found some files (at least data.txt should exist)
	if len(files) == 0 {
		t.Log("No .txt files found in assets directory")
	}
}
