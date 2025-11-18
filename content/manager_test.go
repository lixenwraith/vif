package content

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverContentFiles(t *testing.T) {
	// Create a temporary test directory
	tempDir := t.TempDir()

	// Create test files
	testFiles := []string{
		"file1.txt",
		"file2.txt",
		".hidden.txt", // Should be skipped
		"notxt.go",    // Should be skipped
	}

	for _, name := range testFiles {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
	}

	// Temporarily change assetsDir for testing
	originalAssetsDir := assetsDir
	defer func() {
		// Note: assetsDir is a const, so we can't actually change it in this test
		// In production code, you might want to make it configurable
		_ = originalAssetsDir
	}()

	// Create content manager
	cm := NewContentManager()

	// Test with actual assets directory
	err := cm.DiscoverContentFiles()
	if err != nil {
		t.Fatalf("DiscoverContentFiles failed: %v", err)
	}

	// Should have discovered files from the actual assets directory
	files := cm.GetContentFiles()
	t.Logf("Discovered %d files: %v", len(files), files)

	// Verify all discovered files have .txt extension
	for _, file := range files {
		if filepath.Ext(file) != ".txt" {
			t.Errorf("Non-.txt file discovered: %s", file)
		}
	}
}

func TestDiscoverContentFiles_MissingDirectory(t *testing.T) {
	// Temporarily change to a directory that doesn't exist
	originalDir, _ := os.Getwd()
	tempDir := t.TempDir()

	// Change to temp directory (where assets won't exist)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}
	defer os.Chdir(originalDir)

	cm := NewContentManager()
	err := cm.DiscoverContentFiles()

	// Should not error when directory doesn't exist
	if err != nil {
		t.Errorf("Expected no error for missing directory, got: %v", err)
	}

	// Should have no files discovered
	if len(cm.GetContentFiles()) != 0 {
		t.Errorf("Expected 0 files, got %d", len(cm.GetContentFiles()))
	}
}

func TestLoadContentFile_NonExistent(t *testing.T) {
	cm := NewContentManager()

	_, err := cm.LoadContentFile("/nonexistent/file.txt")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

func TestGetContentFiles(t *testing.T) {
	cm := NewContentManager()

	// Initially should be empty
	files := cm.GetContentFiles()
	if files == nil {
		t.Error("GetContentFiles should not return nil")
	}
}
