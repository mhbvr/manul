package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	var (
		dbFile    = flag.String("db", "./catdb2.db", "Database file path")
		srcDir    = flag.String("src", "", "Source directory containing photo files")
		batchSize = flag.Int("batch-size", 100, "Number of photos to process in each transaction")
	)
	flag.Parse()

	if *srcDir == "" {
		log.Fatal("Source directory must be specified with -src flag")
	}

	builder, err := New(*dbFile)
	if err != nil {
		log.Fatalf("Failed to create database builder: %v", err)
	}
	defer builder.Close()

	fmt.Printf("Scanning directory: %s\n", *srcDir)

	var totalFiles, processedFiles, skippedFiles int

	// First pass: count total files
	err = filepath.Walk(*srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalFiles++
		}
		return nil
	})

	if err != nil {
		log.Fatalf("Failed to count files in source directory: %v", err)
	}

	fmt.Printf("Found %d files to process\n", totalFiles)
	fmt.Printf("Using batch size: %d\n", *batchSize)

	// Collect file paths only
	var filePaths []string
	err = filepath.Walk(*srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		filename := info.Name()
		if _, _, ok := GetIDs(filename); !ok {
			skippedFiles++
			fmt.Printf("Skipping %s: cannot extract cat_id and photo_id\n", filename)
			return nil
		}

		filePaths = append(filePaths, path)
		return nil
	})

	if err != nil {
		log.Fatalf("Failed to collect file paths: %v", err)
	}

	processedFiles = 0
	totalBatches := (len(filePaths) + *batchSize - 1) / *batchSize

	// Process files in streaming batches
	for i := 0; i < len(filePaths); i += *batchSize {
		end := i + *batchSize
		if end > len(filePaths) {
			end = len(filePaths)
		}

		batchPaths := filePaths[i:end]
		batchNum := (i / *batchSize) + 1

		fmt.Printf("Processing batch %d/%d (%d photos)\n", batchNum, totalBatches, len(batchPaths))

		// Read and process this batch
		var batch []PhotoItem
		for _, path := range batchPaths {
			filename := filepath.Base(path)
			catID, photoID, ok := GetIDs(filename)
			if !ok {
				continue
			}

			photoData, err := os.ReadFile(path)
			if err != nil {
				log.Fatalf("Failed to read photo file %s: %v", path, err)
			}

			batch = append(batch, PhotoItem{
				CatID:     catID,
				PhotoID:   photoID,
				FilePath:  path,
				PhotoData: photoData,
			})

			fmt.Printf("  Added photo: cat_id=%d, photo_id=%d, size=%d bytes\n", 
				catID, photoID, len(photoData))
		}

		if err := builder.AddPhotosBatch(batch); err != nil {
			log.Fatalf("Failed to process batch %d: %v", batchNum, err)
		}

		processedFiles += len(batch)
	}

	fmt.Printf("\nDatabase build completed successfully:\n")
	fmt.Printf("  Database file: %s\n", *dbFile)
	fmt.Printf("  Total files found: %d\n", totalFiles)
	fmt.Printf("  Files processed: %d\n", processedFiles)
	fmt.Printf("  Files skipped: %d\n", skippedFiles)

	// Show file size
	if stat, err := os.Stat(*dbFile); err == nil {
		fmt.Printf("  Database size: %d bytes\n", stat.Size())
	}
}

// Reuse the same GetIDs function
func GetIDs(filename string) (catID, photoID uint64, ok bool) {
	var cat, photo uint64
	n, err := fmt.Sscanf(strings.ToLower(filename), "%d_%d.jpg", &cat, &photo)
	if err != nil || n != 2 {
		return 0, 0, false
	}
	return cat, photo, true
}