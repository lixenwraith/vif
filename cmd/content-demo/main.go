package main

import (
	"fmt"
	"log"
	"os"

	"github.com/lixenwraith/vi-fighter/content"
)

func main() {
	fmt.Println("=== Content Discovery Demo ===")

	// Create a new content manager
	cm := content.NewContentManager()

	// Discover all .txt files in the assets directory
	fmt.Println("Discovering content files...")
	err := cm.DiscoverContentFiles()
	if err != nil {
		log.Fatalf("Error discovering files: %v", err)
	}

	// Get the discovered files
	files := cm.GetContentFiles()

	fmt.Printf("\nDiscovered %d content file(s):\n", len(files))
	for i, file := range files {
		fmt.Printf("  [%d] %s\n", i+1, file)
	}

	// Test LoadContentFile stub
	if len(files) > 0 {
		fmt.Printf("\nTesting LoadContentFile stub with: %s\n", files[0])
		_, err := cm.LoadContentFile(files[0])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Println("LoadContentFile stub executed successfully")
		}
	}

	// Test with non-existent file
	fmt.Println("\nTesting error handling with non-existent file...")
	_, err = cm.LoadContentFile("./assets/nonexistent.txt")
	if err != nil {
		fmt.Printf("Expected error received: %v\n", err)
	}

	fmt.Println("\n=== Demo Complete ===")
	os.Exit(0)
}
