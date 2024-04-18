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

func worker(jobs <-chan string, results chan<- map[string]uint32, wg *sync.WaitGroup) {
	defer wg.Done()

	for filePath := range jobs {
		checksum, err := calculateChecksum(filePath)
		if err != nil {
			fmt.Printf("Error calculating checksum for file %s: %v\n", filePath, err)
			continue
		}

		results <- map[string]uint32{filePath: checksum}
	}
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

func getFilesToProcess(mode string, directory string, checksumDB *ChecksumDB) ([]string, error) {
	var filesToProcess []string

	switch mode {
	case "check", "update":
		// Traverse only the files with existing checksums
		for filePath := range checksumDB.Checksums {
			filesToProcess = append(filesToProcess, filePath)
		}
	case "list-missing", "add-missing":
		// Traverse all files on the disk
		err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.IsDir() {
				filesToProcess = append(filesToProcess, path)
			}

			return nil
		})

		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("invalid operation mode: %s", mode)
	}

	return filesToProcess, nil
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
				if storedChecksum, ok := checksumDB.Checksums[filePath]; ok && checksum != storedChecksum {
					fmt.Printf("\r\033[2K") // Move cursor to the beginning of the line and clear the line
					fmt.Printf("Mismatch for file: %s\n", filePath)
				}
			case "update":
				if storedChecksum, ok := checksumDB.Checksums[filePath]; !ok || checksum != storedChecksum {
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
				}
			case "add-missing":
				if _, ok := checksumDB.Checksums[filePath]; !ok {
					checksumDB.mutex.Lock()
					checksumDB.Checksums[filePath] = checksum
					checksumDB.mutex.Unlock()
				}
			}
		}
	}
	close(done)
}

func updateProgressBar(done <-chan struct{}, totalFiles int, processedFiles *uint64, startTime time.Time) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			processed := atomic.LoadUint64(processedFiles)
			elapsed := time.Since(startTime)
			estimatedTotal := time.Duration(int64(elapsed) * int64(totalFiles) / int64(processed))
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
	var directory string
	var dbFilePath string
	var verbose bool
	var mode string
	var numWorkers int
	flag.StringVar(&directory, "dir", "", "Directory to scan")
	flag.StringVar(&dbFilePath, "db", filepath.Join(os.Getenv("HOME"), ".local/lib/checksums.json"), "Checksum database file location")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
	flag.StringVar(&mode, "mode", "", "Operation mode: check, update, list-missing, add-missing")
	flag.IntVar(&numWorkers, "workers", 4, "Number of worker goroutines")
	flag.Parse()

	if directory == "" {
		fmt.Println("Please provide a directory to scan using the -dir flag.")
		os.Exit(1)
	}

	if mode == "" {
		fmt.Println("Please specify an operation mode using the -mode flag: check, update, list-missing, add-missing")
		os.Exit(1)
	}

	checksumDB := loadChecksumDB(dbFilePath, verbose)

	filesToProcess, err := getFilesToProcess(mode, directory, checksumDB)
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
		go worker(jobs, results, &wg)
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
