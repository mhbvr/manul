package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func GetIDs(filename string) (catID, photoID uint64, ok bool) {
	// Simple test implementation: expect format like "cat1_photo2.jpg"
	var cat, photo uint64
	n, err := fmt.Sscanf(strings.ToLower(filename), "%d_%d.jpg", &cat, &photo)
	if err != nil || n != 2 {
		return 0, 0, false
	}
	return cat, photo, true
}

func main() {
	var (
		dbDir  = flag.String("db", "./catdb", "Database directory path")
		srcDir = flag.String("src", "", "Source directory containing photo files")
	)
	flag.Parse()

	if *srcDir == "" {
		log.Fatal("Source directory must be specified with -src flag")
	}

	builder, err := New(*dbDir)
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

	// Second pass: process files
	err = filepath.Walk(*srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		filename := info.Name()
		catID, photoID, ok := GetIDs(filename)
		if !ok {
			skippedFiles++
			fmt.Printf("[%d/%d] Skipping %s: cannot extract cat_id and photo_id\n",
				processedFiles+skippedFiles, totalFiles, filename)
			return nil
		}

		processedFiles++
		fmt.Printf("[%d/%d] Processing %s (cat_id=%d, photo_id=%d)\n",
			processedFiles+skippedFiles, totalFiles, filename, catID, photoID)

		if err := builder.AddPhotoFromFile(catID, photoID, path); err != nil {
			return fmt.Errorf("failed to add photo %s: %w", path, err)
		}

		return nil
	})

	if err != nil {
		log.Fatalf("Failed to traverse source directory: %v", err)
	}

	fmt.Printf("\nDatabase build completed successfully:\n")
	fmt.Printf("  Total files found: %d\n", totalFiles)
	fmt.Printf("  Files processed: %d\n", processedFiles)
	fmt.Printf("  Files skipped: %d\n", skippedFiles)
}
