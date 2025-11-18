package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupLogging_DisabledByDefault(t *testing.T) {
	// Test that logging is disabled when debug=false
	logFile := setupLogging(false)
	if logFile != nil {
		t.Error("Expected nil log file when debug=false")
		logFile.Close()
	}

	// Verify log output is discarded
	output := log.Writer()
	if output != io.Discard {
		t.Errorf("Expected log output to be io.Discard, got %v", output)
	}
}

func TestSetupLogging_EnabledWithDebug(t *testing.T) {
	// Clean up before test
	defer os.RemoveAll(logDir)

	// Test that logging is enabled when debug=true
	logFile := setupLogging(true)
	if logFile == nil {
		t.Fatal("Expected non-nil log file when debug=true")
	}
	defer logFile.Close()

	// Verify logs directory was created
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Error("Expected logs directory to be created")
	}

	// Verify log file was created
	logPath := filepath.Join(logDir, logFileName)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("Expected log file to be created")
	}

	// Write a test log message
	log.Println("Test log message")

	// Verify the log file contains content
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Failed to stat log file: %v", err)
	}
	if info.Size() == 0 {
		t.Error("Expected log file to contain content")
	}
}

func TestSetupLogging_Rotation(t *testing.T) {
	// Clean up before and after test
	defer os.RemoveAll(logDir)

	// Create logs directory
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("Failed to create logs directory: %v", err)
	}

	logPath := filepath.Join(logDir, logFileName)

	// Create a large log file (>10MB)
	largeFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("Failed to create large log file: %v", err)
	}

	// Write just over 10MB
	data := make([]byte, maxLogSize+1)
	if _, err := largeFile.Write(data); err != nil {
		t.Fatalf("Failed to write to log file: %v", err)
	}
	largeFile.Close()

	// Setup logging, which should trigger rotation
	logFile := setupLogging(true)
	if logFile == nil {
		t.Fatal("Expected non-nil log file")
	}
	defer logFile.Close()

	// Verify the old log file was rotated (a file with timestamp should exist)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("Failed to read logs directory: %v", err)
	}

	rotatedFound := false
	for _, entry := range entries {
		if entry.Name() != logFileName && filepath.Ext(entry.Name()) == ".log" {
			rotatedFound = true
			break
		}
	}

	if !rotatedFound {
		t.Error("Expected to find rotated log file")
	}

	// Verify new log file is smaller
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Failed to stat new log file: %v", err)
	}
	if info.Size() > maxLogSize {
		t.Errorf("Expected new log file to be smaller than %d bytes, got %d", maxLogSize, info.Size())
	}
}

func TestSetupLogging_NoStdoutStderr(t *testing.T) {
	// Clean up after test
	defer os.RemoveAll(logDir)

	// Setup logging
	logFile := setupLogging(true)
	if logFile == nil {
		t.Fatal("Expected non-nil log file")
	}
	defer logFile.Close()

	// Verify log output is not stdout or stderr
	output := log.Writer()
	if output == os.Stdout {
		t.Error("Log output should not be stdout")
	}
	if output == os.Stderr {
		t.Error("Log output should not be stderr")
	}
}
