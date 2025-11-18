package content

import (
	"testing"
)

// TestIntegrationWithRealFiles tests the content processing with actual files in assets directory
func TestIntegrationWithRealFiles(t *testing.T) {
	cm := NewContentManager()

	// Discover content files
	err := cm.DiscoverContentFiles()
	if err != nil {
		t.Fatalf("Failed to discover content files: %v", err)
	}

	files := cm.GetContentFiles()
	t.Logf("Discovered %d content files", len(files))

	if len(files) == 0 {
		t.Skip("No content files available for integration test")
	}

	// Test SelectRandomBlockWithValidation with real files
	for i := 0; i < 10; i++ {
		block, filePath, err := cm.SelectRandomBlockWithValidation()
		if err != nil {
			t.Fatalf("Iteration %d: SelectRandomBlockWithValidation failed: %v", i, err)
		}

		t.Logf("Iteration %d: Got %d lines from %s", i, len(block), filePath)

		// Verify the content is valid
		if !cm.ValidateProcessedContent(block) {
			t.Errorf("Iteration %d: Content from %s failed validation", i, filePath)
		}

		// Check line lengths
		for lineNum, line := range block {
			if len(line) > MaxLineLength {
				t.Errorf("Iteration %d, Line %d: exceeds max length (%d > %d): %q",
					i, lineNum, len(line), MaxLineLength, line)
			}

			if len(line) == 0 {
				t.Errorf("Iteration %d, Line %d: is empty", i, lineNum)
			}

			// Check for comments that should have been filtered
			if cm.isCommentLine(line) {
				t.Errorf("Iteration %d, Line %d: contains unfiltered comment: %q", i, lineNum, line)
			}
		}
	}
}

// TestProcessingRealFileContent tests processing of actual file content
func TestProcessingRealFileContent(t *testing.T) {
	cm := NewContentManager()

	// Discover content files
	err := cm.DiscoverContentFiles()
	if err != nil {
		t.Fatalf("Failed to discover content files: %v", err)
	}

	files := cm.GetContentFiles()
	if len(files) == 0 {
		t.Skip("No content files available for processing test")
	}

	// Test each file
	for _, filePath := range files {
		t.Run(filePath, func(t *testing.T) {
			// Get a block from the file
			block, err := cm.GetContentBlock(filePath, 0, ContentBlockSize)
			if err != nil {
				t.Fatalf("Failed to get content block: %v", err)
			}

			if len(block) == 0 {
				t.Logf("File %s has no valid content lines", filePath)
				return
			}

			// Process the block
			processed := cm.ProcessContentBlock(block)

			t.Logf("File %s: Original %d lines, Processed %d lines",
				filePath, len(block), len(processed))

			// Verify processing
			for i, line := range processed {
				// Should not be empty
				if len(line) == 0 {
					t.Errorf("Processed line %d is empty", i)
				}

				// Should not be a comment
				if cm.isCommentLine(line) {
					t.Errorf("Processed line %d is still a comment: %q", i, line)
				}

				// Should not exceed max length
				if len(line) > MaxLineLength {
					t.Errorf("Processed line %d exceeds max length (%d > %d): %q",
						i, len(line), MaxLineLength, line)
				}

				// Should be trimmed (no leading/trailing whitespace)
				trimmed := line
				if line != trimmed {
					t.Errorf("Processed line %d has untrimmed whitespace: %q", i, line)
				}
			}
		})
	}
}
