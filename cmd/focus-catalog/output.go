package main

import (
	"bufio"
	"fmt"
	"os"
)

// WriteOutputFile writes file paths to catalog output file.
func WriteOutputFile(path string, files []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, file := range files {
		fmt.Fprintf(w, "./%s\n", file)
	}
	return w.Flush()
}