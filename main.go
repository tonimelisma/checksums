// checksumtool - A tool for calculating and comparing file checksums
// Copyright (C) 2024 Toni Melisma
//
// TODO exit on ctrl-C
// TODO check for deleted files when comparing checksums
// TODO delete deleted files from checksum db when adding/updating

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type ChecksumDB struct {
	Checksums map[string]uint32 `json:"checksums"`
	mutex     sync.Mutex
}

func calculateChecksum(filePath string) (uint32, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	hash := crc32.NewIEEE()
	if _, err := io.Copy(hash, file); err != nil {
		return 0, err
	}

	return hash.Sum32(), nil
}

func loadChecksumDB(dbFilePath string, verbose bool) *ChecksumDB {
	if verbose {
		fmt.Println("Loading checksum database...")
	}

	checksumDB := &ChecksumDB{Checksums: make(map[string]uint32)}
	dbData, err := os.ReadFile(dbFilePath)
	if err == nil {
		json.Unmarshal(dbData, checksumDB)
	}

	if verbose {
		fmt.Printf("Checksum database loaded with %d files\n", len(checksumDB.Checksums))
		fmt.Println("Comparing checksums with the database...")
	}

	return checksumDB
}

func getFilesToProcess(mode string, directories []string, checksumDB *ChecksumDB) ([]string, bool, error) {
	var filesToProcess []string
	var calculateChecksums bool

	switch mode {
	case "check", "update":
		// Traverse only the files with existing checksums
		calculateChecksums = true
		for filePath := range checksumDB.Checksums {
			absPath, err := filepath.Abs(filePath)
			if err != nil {
				return nil, false, fmt.Errorf("failed to get absolute path for %s: %v", filePath, err)
			}
			filesToProcess = append(filesToProcess, absPath)
		}
	case "list-missing", "add-missing":
		// Traverse all files in the specified directories
		calculateChecksums = mode == "add-missing"
		for _, directory := range directories {
			err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				if !info.IsDir() {
					absPath, err := filepath.Abs(path)
					if err != nil {
						return fmt.Errorf("failed to get absolute path for %s: %v", path, err)
					}
					if mode == "add-missing" {
						if _, ok := checksumDB.Checksums[absPath]; !ok {
							filesToProcess = append(filesToProcess, absPath)
						}
					} else {
						filesToProcess = append(filesToProcess, absPath)
					}
				}

				return nil
			})

			if err != nil {
				return nil, false, err
			}
		}
	default:
		return nil, false, fmt.Errorf("invalid operation mode: %s", mode)
	}

	return filesToProcess, calculateChecksums, nil
}

func worker(jobs <-chan string, results chan<- map[string]uint32, calculateChecksums bool, wg *sync.WaitGroup) {
	defer wg.Done()

	for filePath := range jobs {
		if !fileExists(filePath) {
			results <- map[string]uint32{filePath: 0}
			continue
		}

		var checksum uint32
		var err error

		if calculateChecksums {
			checksum, err = calculateChecksum(filePath)
			if err != nil {
				fmt.Printf("Error calculating checksum for file %s: %v\n", filePath, err)
				continue
			}
		}

		results <- map[string]uint32{filePath: checksum}
	}
}

func saveChecksumDB(dbFilePath string, checksumDB *ChecksumDB, verbose bool) {
	if verbose {
		fmt.Println("Saving checksum database...")
	}

	checksumDB.mutex.Lock()
	dbData, err := json.MarshalIndent(checksumDB, "", "  ")
	checksumDB.mutex.Unlock()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	err = os.WriteFile(dbFilePath, dbData, 0644)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	if verbose {
		fmt.Println("Checksum database saved.")
	}
}

func processResults(results <-chan map[string]uint32, done chan<- struct{}, mode string, checksumDB *ChecksumDB, processedFiles *uint64) {
	for result := range results {
		for filePath, checksum := range result {
			atomic.AddUint64(processedFiles, 1)

			switch mode {
			case "check":
				if storedChecksum, ok := checksumDB.Checksums[filePath]; !ok {
					fmt.Printf("\r\033[2K") // Move cursor to the beginning of the line and clear the line
					fmt.Printf("File not in database: %s\n", filePath)
				} else if checksum == 0 {
					fmt.Printf("\r\033[2K") // Move cursor to the beginning of the line and clear the line
					fmt.Printf("File missing: %s\n", filePath)
				} else if checksum != storedChecksum {
					fmt.Printf("\r\033[2K") // Move cursor to the beginning of the line and clear the line
					fmt.Printf("Mismatch for file: %s\n", filePath)
				}
			case "update":
				if checksum == 0 {
					fmt.Printf("\r\033[2K") // Move cursor to the beginning of the line and clear the line
					fmt.Printf("File missing: %s\n", filePath)
					delete(checksumDB.Checksums, filePath)
				} else if storedChecksum, ok := checksumDB.Checksums[filePath]; !ok || checksum != storedChecksum {
					fmt.Printf("\r\033[2K") // Move cursor to the beginning of the line and clear the line
					fmt.Printf("Changed or new file: %s\n", filePath)
					checksumDB.mutex.Lock()
					checksumDB.Checksums[filePath] = checksum
					checksumDB.mutex.Unlock()
				}
			case "list-missing":
				if _, ok := checksumDB.Checksums[filePath]; !ok {
					fmt.Printf("\r\033[2K") // Move cursor to the beginning of the line and clear the line
					fmt.Printf("Missing checksum: %s\n", filePath)
				} else if checksum == 0 {
					fmt.Printf("\r\033[2K") // Move cursor to the beginning of the line and clear the line
					fmt.Printf("File missing: %s\n", filePath)
				}
			case "add-missing":
				if _, ok := checksumDB.Checksums[filePath]; !ok {
					if checksum != 0 {
						checksumDB.mutex.Lock()
						checksumDB.Checksums[filePath] = checksum
						checksumDB.mutex.Unlock()
					} else {
						fmt.Printf("\r\033[2K") // Move cursor to the beginning of the line and clear the line
						fmt.Printf("File missing: %s\n", filePath)
					}
				}
			}
		}
	}
	close(done)
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

func updateProgressBar(done <-chan struct{}, totalFiles int, processedFiles *uint64, startTime time.Time) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			processed := atomic.LoadUint64(processedFiles)
			elapsed := time.Since(startTime)
			estimatedTotal := time.Duration(int64(0))
			if processed > 0 {
				estimatedTotal = time.Duration(int64(elapsed) * int64(totalFiles) / int64(processed))
			}
			elapsedHuman := formatDuration(elapsed)
			estimatedTotalHuman := formatDuration(estimatedTotal)
			fmt.Printf("\r\033[2K") // Move cursor to the beginning of the line and clear the line
			fmt.Printf("Processed %d/%d files (Elapsed: %s, Estimated Total: %s)", processed, totalFiles, elapsedHuman, estimatedTotalHuman)
		case <-done:
			fmt.Println()
			return
		}
	}
}

func handleInterrupt(interruptChan chan os.Signal, dbFilePath string, checksumDB *ChecksumDB, verbose bool) {
	<-interruptChan
	fmt.Println("\nInterrupt signal received. Saving work done so far...")
	saveChecksumDB(dbFilePath, checksumDB, verbose)
	os.Exit(0)
}

func main() {
	var dbFilePath string
	var verbose bool
	var mode string
	var numWorkers int
	flag.StringVar(&dbFilePath, "db", filepath.Join(os.Getenv("HOME"), ".local/lib/checksums.json"), "Checksum database file location")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
	flag.StringVar(&mode, "mode", "", "Operation mode: check, update, list-missing, add-missing")
	flag.IntVar(&numWorkers, "workers", 4, "Number of worker goroutines")
	flag.Parse()

	directories := flag.Args()
	if len(directories) == 0 {
		fmt.Println("Please provide one or more directories to scan.")
		os.Exit(1)
	}

	if mode == "" {
		fmt.Println("Please specify an operation mode using the -mode flag: check, update, list-missing, add-missing")
		os.Exit(1)
	}

	checksumDB := loadChecksumDB(dbFilePath, verbose)

	filesToProcess, calculateChecksums, err := getFilesToProcess(mode, directories, checksumDB)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	totalFiles := len(filesToProcess)
	var processedFiles uint64
	startTime := time.Now()

	jobs := make(chan string, numWorkers)
	results := make(chan map[string]uint32, numWorkers)
	var wg sync.WaitGroup

	// Create worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(jobs, results, calculateChecksums, &wg)
	}

	// Handle interrupt signal in a separate goroutine
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM)
	go handleInterrupt(interruptChan, dbFilePath, checksumDB, verbose)

	// Feed file paths to the input channel
	go func() {
		for _, filePath := range filesToProcess {
			jobs <- filePath
		}
		close(jobs)
	}()

	// Process results in a separate goroutine
	done := make(chan struct{})
	go processResults(results, done, mode, checksumDB, &processedFiles)

	// Update progress bar in a separate goroutine
	go updateProgressBar(done, totalFiles, &processedFiles, startTime)

	// Wait for all worker goroutines to finish
	wg.Wait()
	close(results)

	// Wait for the result processing goroutine to finish
	<-done

	if verbose {
		fmt.Printf("\nFinished operation in '%s' mode.\n", mode)
	}

	// Save the updated checksum database if necessary
	if mode == "update" || mode == "add-missing" {
		saveChecksumDB(dbFilePath, checksumDB, verbose)
	}
}

// formatDuration formats a time.Duration into a human-readable string
// without decimal places, showing hours, minutes, and seconds as relevant.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	d -= minutes * time.Minute
	seconds := d / time.Second
	if hours > 0 {
		return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	} else {
		return fmt.Sprintf("%ds", seconds)
	}
}
