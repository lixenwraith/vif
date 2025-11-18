package main

import (
	"fmt"
	"log"

	"github.com/lixenwraith/vi-fighter/content"
)

func main() {
	log.SetFlags(log.Ltime)

	cm := content.NewContentManager()

	// Discover content files
	fmt.Println("=== Discovering content files ===")
	err := cm.DiscoverContentFiles()
	if err != nil {
		log.Fatalf("Failed to discover content files: %v", err)
	}

	files := cm.GetContentFiles()
	fmt.Printf("Discovered %d content files:\n", len(files))
	for i, file := range files {
		fmt.Printf("  %d. %s\n", i+1, file)
	}
	fmt.Println()

	// Test SelectRandomBlockWithValidation
	fmt.Println("=== Testing SelectRandomBlockWithValidation ===")
	for i := 0; i < 5; i++ {
		fmt.Printf("\n--- Attempt %d ---\n", i+1)
		block, filePath, err := cm.SelectRandomBlockWithValidation()
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}

		fmt.Printf("Source: %s\n", filePath)
		fmt.Printf("Lines: %d\n", len(block))
		fmt.Printf("Valid: %v\n", cm.ValidateProcessedContent(block))

		// Show first few lines
		fmt.Println("Preview:")
		for j := 0; j < 5 && j < len(block); j++ {
			fmt.Printf("  [%2d] %s\n", j+1, block[j])
		}
		if len(block) > 5 {
			fmt.Printf("  ... (%d more lines)\n", len(block)-5)
		}
	}

	// Test ProcessContentBlock with various inputs
	fmt.Println("\n=== Testing ProcessContentBlock ===")

	testCases := []struct {
		name  string
		lines []string
	}{
		{
			name: "Mixed content with comments",
			lines: []string{
				"// Comment line",
				"Valid line 1",
				"",
				"# Another comment",
				"   Valid line 2 with spaces   ",
				"Valid line 3",
			},
		},
		{
			name: "All comments",
			lines: []string{
				"// Comment 1",
				"# Comment 2",
				"// Comment 3",
			},
		},
	}

	for _, tc := range testCases {
		fmt.Printf("\nTest: %s\n", tc.name)
		fmt.Printf("Input: %d lines\n", len(tc.lines))

		processed := cm.ProcessContentBlock(tc.lines)
		fmt.Printf("Output: %d lines\n", len(processed))
		fmt.Printf("Valid: %v\n", cm.ValidateProcessedContent(processed))

		for i, line := range processed {
			fmt.Printf("  [%d] %s\n", i+1, line)
		}
	}

	fmt.Println("\n=== Test complete ===")
}
