package main

import (
	"encoding/json"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestCalculateChecksum(t *testing.T) {
	// Create a temporary file for testing
	tempFile, err := os.CreateTemp("", "testfile")
	if err != nil {
		t.Fatalf("Failed to create temporary file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	// Write some content to the file
	content := []byte("Hello, world!")
	if _, err := tempFile.Write(content); err != nil {
		t.Fatalf("Failed to write content to temporary file: %v", err)
	}
	tempFile.Close()

	// Calculate the checksum of the file
	checksum, err := calculateChecksum(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to calculate checksum: %v", err)
	}

	// Define the expected checksum value
	expectedChecksum := uint32(3957769958)

	// Compare the calculated checksum with the expected value
	if checksum != expectedChecksum {
		t.Errorf("Checksum mismatch. Expected: %d, Got: %d", expectedChecksum, checksum)
	}
}

func TestWorker(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "testdir")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a temporary file in the directory
	tempFile, err := os.CreateTemp(tempDir, "testfile")
	if err != nil {
		t.Fatalf("Failed to create temporary file: %v", err)
	}
	tempFile.Close()

	// Create channels and wait group for testing
	jobs := make(chan string, 1)
	results := make(chan map[string]uint32, 1)
	var wg sync.WaitGroup

	// Add the temporary file path to the jobs channel
	jobs <- tempFile.Name()
	close(jobs)

	// Start the worker goroutine
	wg.Add(1)
	go worker(jobs, results, true, &wg)

	// Wait for the worker to finish
	wg.Wait()

	// Read the result from the results channel
	result := <-results

	// Check if the result contains the expected file path
	if _, ok := result[tempFile.Name()]; !ok {
		t.Errorf("Expected file path not found in the result")
	}
}

func TestLoadChecksumDB(t *testing.T) {
	// Create a temporary file for testing
	tempFile, err := os.CreateTemp("", "testdb")
	if err != nil {
		t.Fatalf("Failed to create temporary file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	// Write some content to the file
	content := []byte(`{"checksums":{"file1":1234,"file2":5678}}`)
	if _, err := tempFile.Write(content); err != nil {
		t.Fatalf("Failed to write content to temporary file: %v", err)
	}
	tempFile.Close()

	// Load the checksum database
	checksumDB := loadChecksumDB(tempFile.Name(), false)

	// Define the expected checksum database
	expectedDB := &ChecksumDB{
		Checksums: map[string]uint32{
			"file1": 1234,
			"file2": 5678,
		},
	}

	// Compare the loaded checksum database with the expected database
	if !reflect.DeepEqual(checksumDB, expectedDB) {
		t.Errorf("Checksum database mismatch. Expected: %v, Got: %v", expectedDB, checksumDB)
	}
}

func TestGetFilesToProcess(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "testdir")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create temporary files in the directory
	file1, err := os.CreateTemp(tempDir, "file1")
	if err != nil {
		t.Fatalf("Failed to create temporary file: %v", err)
	}
	file1.Close()

	file2, err := os.CreateTemp(tempDir, "file2")
	if err != nil {
		t.Fatalf("Failed to create temporary file: %v", err)
	}
	file2.Close()

	// Create a checksum database with one file
	checksumDB := &ChecksumDB{
		Checksums: map[string]uint32{
			file1.Name(): 1234,
		},
	}

	// Test case 1: "check" mode
	files, calculateChecksums, err := getFilesToProcess("check", []string{tempDir}, checksumDB)
	if err != nil {
		t.Errorf("Unexpected error in 'check' mode: %v", err)
	}
	expectedFiles := []string{file1.Name()}
	if !reflect.DeepEqual(files, expectedFiles) {
		t.Errorf("File list mismatch in 'check' mode. Expected: %v, Got: %v", expectedFiles, files)
	}
	if !calculateChecksums {
		t.Error("Expected calculateChecksums to be true in 'check' mode")
	}

	// Test case 2: "update" mode
	files, calculateChecksums, err = getFilesToProcess("update", []string{tempDir}, checksumDB)
	if err != nil {
		t.Errorf("Unexpected error in 'update' mode: %v", err)
	}
	expectedFiles = []string{file1.Name()}
	if !reflect.DeepEqual(files, expectedFiles) {
		t.Errorf("File list mismatch in 'update' mode. Expected: %v, Got: %v", expectedFiles, files)
	}
	if !calculateChecksums {
		t.Error("Expected calculateChecksums to be true in 'update' mode")
	}

	// Test case 3: "list-missing" mode
	files, calculateChecksums, err = getFilesToProcess("list-missing", []string{tempDir}, checksumDB)
	if err != nil {
		t.Errorf("Unexpected error in 'list-missing' mode: %v", err)
	}
	expectedFiles = []string{file1.Name(), file2.Name()}
	if !reflect.DeepEqual(files, expectedFiles) {
		t.Errorf("File list mismatch in 'list-missing' mode. Expected: %v, Got: %v", expectedFiles, files)
	}
	if calculateChecksums {
		t.Error("Expected calculateChecksums to be false in 'list-missing' mode")
	}

	// Test case 4: "add-missing" mode
	files, calculateChecksums, err = getFilesToProcess("add-missing", []string{tempDir}, checksumDB)
	if err != nil {
		t.Errorf("Unexpected error in 'add-missing' mode: %v", err)
	}
	expectedFiles = []string{file2.Name()}
	if !reflect.DeepEqual(files, expectedFiles) {
		t.Errorf("File list mismatch in 'add-missing' mode. Expected: %v, Got: %v", expectedFiles, files)
	}
	if !calculateChecksums {
		t.Error("Expected calculateChecksums to be true in 'add-missing' mode")
	}

	// Test case 5: invalid mode
	_, _, err = getFilesToProcess("invalid", []string{tempDir}, checksumDB)
	if err == nil {
		t.Error("Expected an error for invalid mode, but got nil")
	}
}

func TestSaveChecksumDB(t *testing.T) {
	// Create a temporary file for testing
	tempFile, err := os.CreateTemp("", "testdb")
	if err != nil {
		t.Fatalf("Failed to create temporary file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	// Create a checksum database
	checksumDB := &ChecksumDB{
		Checksums: map[string]uint32{
			"file1": 1234,
			"file2": 5678,
		},
	}

	// Save the checksum database
	saveChecksumDB(tempFile.Name(), checksumDB, false)

	// Read the saved checksum database file
	content, err := os.ReadFile(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to read saved checksum database file: %v", err)
	}

	// Parse the saved content into a map
	var savedDB map[string]map[string]uint32
	err = json.Unmarshal(content, &savedDB)
	if err != nil {
		t.Fatalf("Failed to parse saved checksum database: %v", err)
	}

	// Define the expected content
	expectedDB := map[string]map[string]uint32{
		"checksums": {
			"file1": 1234,
			"file2": 5678,
		},
	}

	// Compare the saved content with the expected content
	if !reflect.DeepEqual(savedDB, expectedDB) {
		t.Errorf("Saved checksum database content mismatch. Expected: %v, Got: %v", expectedDB, savedDB)
	}
}

func TestProcessResults(t *testing.T) {
	// Create a checksum database for testing
	checksumDB := &ChecksumDB{
		Checksums: map[string]uint32{
			"file1": 1234,
			"file2": 5678,
		},
	}

	// Create channels and wait group for testing
	results := make(chan map[string]uint32, 3)
	done := make(chan struct{})
	var processedFiles uint64

	// Test case 1: "check" mode
	results <- map[string]uint32{"file1": 1234}
	results <- map[string]uint32{"file2": 0}
	results <- map[string]uint32{"file3": 9012}
	close(results)

	processResults(results, done, "check", checksumDB, &processedFiles)
	<-done

	// Test case 2: "update" mode
	results = make(chan map[string]uint32, 3)
	done = make(chan struct{})
	processedFiles = 0

	results <- map[string]uint32{"file1": 1234}
	results <- map[string]uint32{"file2": 0}
	results <- map[string]uint32{"file3": 9012}
	close(results)

	processResults(results, done, "update", checksumDB, &processedFiles)
	<-done

	// Check if the missing file is removed from the checksum database
	if _, ok := checksumDB.Checksums["file2"]; ok {
		t.Error("Missing file should have been removed from the checksum database")
	}

	// Check if the new file is added to the checksum database
	if checksumDB.Checksums["file3"] != 9012 {
		t.Error("New file should have been added to the checksum database")
	}

	// Test case 3: "list-missing" mode
	results = make(chan map[string]uint32, 2)
	done = make(chan struct{})
	processedFiles = 0

	results <- map[string]uint32{"file1": 0}
	results <- map[string]uint32{"file4": 0}
	close(results)

	processResults(results, done, "list-missing", checksumDB, &processedFiles)
	<-done

	// Test case 4: "add-missing" mode
	results = make(chan map[string]uint32, 2)
	done = make(chan struct{})
	processedFiles = 0

	results <- map[string]uint32{"file4": 3456}
	results <- map[string]uint32{"file5": 0}
	close(results)

	processResults(results, done, "add-missing", checksumDB, &processedFiles)
	<-done

	// Check if the new file is added to the checksum database
	if checksumDB.Checksums["file4"] != 3456 {
		t.Error("New file should have been added to the checksum database")
	}
}

func TestFileExists(t *testing.T) {
	// Create a temporary file for testing
	tempFile, err := os.CreateTemp("", "testfile")
	if err != nil {
		t.Fatalf("Failed to create temporary file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.Close()

	// Test case 1: file exists
	if !fileExists(tempFile.Name()) {
		t.Error("Expected fileExists to return true for an existing file")
	}

	// Test case 2: file doesn't exist
	if fileExists("nonexistent_file") {
		t.Error("Expected fileExists to return false for a nonexistent file")
	}
}

func TestFormatDuration(t *testing.T) {
	// Test cases
	testCases := []struct {
		duration time.Duration
		expected string
	}{
		{time.Second * 30, "30s"},
		{time.Minute * 2, "2m0s"},
		{time.Hour*1 + time.Minute*30 + time.Second*15, "1h30m15s"},
	}

	// Run test cases
	for _, tc := range testCases {
		result := formatDuration(tc.duration)
		if result != tc.expected {
			t.Errorf("Formatted duration mismatch. Expected: %s, Got: %s", tc.expected, result)
		}
	}
}
