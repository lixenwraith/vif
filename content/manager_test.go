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

func TestIsCommentLine(t *testing.T) {
	cm := NewContentManager()

	tests := []struct {
		line     string
		expected bool
	}{
		{"// This is a comment", true},
		{"# This is also a comment", true},
		{"   // Comment with leading spaces", true},
		{"   # Comment with leading spaces", true},
		{"This is not a comment", false},
		{"func main() {", false},
		{"", false},
		{"   ", false},
	}

	for _, test := range tests {
		result := cm.isCommentLine(test.line)
		if result != test.expected {
			t.Errorf("isCommentLine(%q) = %v, expected %v", test.line, result, test.expected)
		}
	}
}

func TestIsValidContentLine(t *testing.T) {
	cm := NewContentManager()

	tests := []struct {
		line     string
		expected bool
	}{
		{"This is valid content", true},
		{"func main() {", true},
		{"// This is a comment", false},
		{"# This is a comment", false},
		{"", false},
		{"   ", false},
		{"   // Comment", false},
	}

	for _, test := range tests {
		result := cm.isValidContentLine(test.line)
		if result != test.expected {
			t.Errorf("isValidContentLine(%q) = %v, expected %v", test.line, result, test.expected)
		}
	}
}

func TestGetContentBlock(t *testing.T) {
	// Create a temporary test file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")

	content := `// This is a comment
Line 1
Line 2

// Another comment
Line 3
Line 4
Line 5

# Python-style comment
Line 6
Line 7
`

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cm := NewContentManager()

	// Test getting a block from the beginning
	block, err := cm.GetContentBlock(testFile, 0, 3)
	if err != nil {
		t.Fatalf("GetContentBlock failed: %v", err)
	}

	if len(block) != 3 {
		t.Errorf("Expected 3 lines, got %d", len(block))
	}

	// Verify it skipped comments and empty lines
	if block[0] != "Line 1" {
		t.Errorf("Expected 'Line 1', got %q", block[0])
	}

	// Test wrapping around
	block, err = cm.GetContentBlock(testFile, 6, 3)
	if err != nil {
		t.Fatalf("GetContentBlock failed: %v", err)
	}

	// Should wrap around (there are 7 valid lines total)
	if len(block) != 3 {
		t.Errorf("Expected 3 lines with wrapping, got %d", len(block))
	}
}

func TestSelectRandomBlock(t *testing.T) {
	// Create a temporary test directory with files
	tempDir := t.TempDir()

	testFiles := map[string]string{
		"file1.txt": `Line 1
Line 2
Line 3
Line 4
Line 5`,
		"file2.txt": `// Comment
Other Line 1
Other Line 2

Other Line 3`,
	}

	for name, content := range testFiles {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
	}

	cm := NewContentManager()

	// Manually set content files for testing
	cm.contentFiles = []string{
		filepath.Join(tempDir, "file1.txt"),
		filepath.Join(tempDir, "file2.txt"),
	}

	// Test selecting a random block
	block, filePath, err := cm.SelectRandomBlock()
	if err != nil {
		t.Fatalf("SelectRandomBlock failed: %v", err)
	}

	if len(block) == 0 {
		t.Error("Expected non-empty block")
	}

	if filePath == "" {
		t.Error("Expected non-empty file path")
	}

	t.Logf("Selected block from %s with %d lines", filePath, len(block))
	t.Logf("Block content: %v", block)
}

func TestSelectRandomBlock_NoFiles(t *testing.T) {
	cm := NewContentManager()

	// No files discovered
	_, _, err := cm.SelectRandomBlock()
	if err == nil {
		t.Error("Expected error when no files discovered, got nil")
	}
}
