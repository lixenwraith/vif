package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestProcessContentBlock(t *testing.T) {
	cm := NewContentManager()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name: "Remove comments and empty lines",
			input: []string{
				"// This is a comment",
				"Line 1",
				"",
				"# Another comment",
				"Line 2",
				"   ",
				"Line 3",
			},
			expected: []string{"Line 1", "Line 2", "Line 3"},
		},
		{
			name: "Trim whitespace",
			input: []string{
				"   Line with leading spaces",
				"Line with trailing spaces   ",
				"  Line with both  ",
			},
			expected: []string{
				"Line with leading spaces",
				"Line with trailing spaces",
				"Line with both",
			},
		},
		{
			name: "Truncate long lines",
			input: []string{
				strings.Repeat("x", 100), // 100 characters
			},
			expected: []string{
				strings.Repeat("x", MaxLineLength), // Should be truncated to MaxLineLength
			},
		},
		{
			name: "All comments",
			input: []string{
				"// Comment 1",
				"# Comment 2",
				"// Comment 3",
			},
			expected: []string{},
		},
		{
			name: "All empty",
			input: []string{
				"",
				"   ",
				"\t",
			},
			expected: []string{},
		},
		{
			name:     "Empty input",
			input:    []string{},
			expected: []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := cm.ProcessContentBlock(test.input)

			if len(result) != len(test.expected) {
				t.Errorf("Expected %d lines, got %d", len(test.expected), len(result))
				return
			}

			for i, line := range result {
				if line != test.expected[i] {
					t.Errorf("Line %d: expected %q, got %q", i, test.expected[i], line)
				}
			}
		})
	}
}

func TestValidateProcessedContent(t *testing.T) {
	cm := NewContentManager()

	tests := []struct {
		name     string
		input    []string
		expected bool
	}{
		{
			name:     "Valid content - exactly minimum lines",
			input:    generateLines(MinProcessedLines),
			expected: true,
		},
		{
			name:     "Valid content - more than minimum",
			input:    generateLines(MinProcessedLines + 5),
			expected: true,
		},
		{
			name:     "Invalid - too few lines",
			input:    generateLines(MinProcessedLines - 1),
			expected: false,
		},
		{
			name:     "Invalid - empty content",
			input:    []string{},
			expected: false,
		},
		{
			name: "Invalid - line too long",
			input: append(
				generateLines(MinProcessedLines-1),
				strings.Repeat("x", MaxLineLength+1),
			),
			expected: false,
		},
		{
			name:     "Valid - lines at max length",
			input:    generateLines(MinProcessedLines, MaxLineLength),
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := cm.ValidateProcessedContent(test.input)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestGetDefaultContent(t *testing.T) {
	cm := NewContentManager()

	content := cm.GetDefaultContent()

	// Should have at least minimum required lines
	if len(content) < MinProcessedLines {
		t.Errorf("Default content has %d lines, expected at least %d", len(content), MinProcessedLines)
	}

	// All lines should be non-empty
	for i, line := range content {
		if len(line) == 0 {
			t.Errorf("Line %d is empty", i)
		}
		if len(line) > MaxLineLength {
			t.Errorf("Line %d exceeds max length (%d > %d)", i, len(line), MaxLineLength)
		}
	}

	// Default content should pass validation
	if !cm.ValidateProcessedContent(content) {
		t.Error("Default content failed validation")
	}
}

func TestSelectRandomBlockWithValidation(t *testing.T) {
	// Create a temporary test directory with files
	tempDir := t.TempDir()

	testFiles := map[string]string{
		"valid.txt": generateFileContent(MinProcessedLines + 5),
		"invalid.txt": `// Just comments
# More comments
// Nothing useful`,
		"mixed.txt": `// Header comment
` + generateFileContent(MinProcessedLines) + `
// Footer comment`,
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
		filepath.Join(tempDir, "valid.txt"),
		filepath.Join(tempDir, "invalid.txt"),
		filepath.Join(tempDir, "mixed.txt"),
	}

	// Test selecting with validation
	block, filePath, err := cm.SelectRandomBlockWithValidation()
	if err != nil {
		t.Fatalf("SelectRandomBlockWithValidation failed: %v", err)
	}

	// Should get valid content
	if len(block) < MinProcessedLines {
		t.Errorf("Expected at least %d lines, got %d", MinProcessedLines, len(block))
	}

	// Should pass validation
	if !cm.ValidateProcessedContent(block) {
		t.Error("Returned content failed validation")
	}

	t.Logf("Selected block from %s with %d lines", filePath, len(block))
}

func TestSelectRandomBlockWithValidation_FallbackToDefault(t *testing.T) {
	cm := NewContentManager()

	// No files discovered - should fall back to default
	block, filePath, err := cm.SelectRandomBlockWithValidation()
	if err != nil {
		t.Fatalf("SelectRandomBlockWithValidation failed: %v", err)
	}

	if filePath != "default" {
		t.Errorf("Expected 'default' as file path, got %q", filePath)
	}

	// Should get default content
	defaultContent := cm.GetDefaultContent()
	if len(block) != len(defaultContent) {
		t.Errorf("Expected default content with %d lines, got %d", len(defaultContent), len(block))
	}

	// Should pass validation
	if !cm.ValidateProcessedContent(block) {
		t.Error("Default content failed validation")
	}
}

func TestSelectRandomBlockWithValidation_AllInvalid(t *testing.T) {
	// Create a temporary test directory with only invalid files
	tempDir := t.TempDir()

	testFiles := map[string]string{
		"comments1.txt": `// Just comments
# More comments`,
		"comments2.txt": `# All comments here
// Nothing to see`,
		"empty.txt": `


`,
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
		filepath.Join(tempDir, "comments1.txt"),
		filepath.Join(tempDir, "comments2.txt"),
		filepath.Join(tempDir, "empty.txt"),
	}

	// Should fall back to default content after max retries
	block, filePath, err := cm.SelectRandomBlockWithValidation()
	if err != nil {
		t.Fatalf("SelectRandomBlockWithValidation failed: %v", err)
	}

	if filePath != "default" {
		t.Errorf("Expected 'default' as file path after retries, got %q", filePath)
	}

	// Should get valid default content
	if !cm.ValidateProcessedContent(block) {
		t.Error("Fallback content failed validation")
	}
}

// Helper function to generate test lines
func generateLines(count int, lengths ...int) []string {
	length := 10 // default length
	if len(lengths) > 0 {
		length = lengths[0]
	}

	lines := make([]string, count)
	for i := 0; i < count; i++ {
		lines[i] = strings.Repeat("x", length)
	}
	return lines
}

// Helper function to generate file content
func generateFileContent(lineCount int) string {
	lines := make([]string, lineCount)
	for i := 0; i < lineCount; i++ {
		lines[i] = fmt.Sprintf("Line %d with some content", i+1)
	}
	return strings.Join(lines, "\n")
}
