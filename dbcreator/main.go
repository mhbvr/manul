package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/mhbvr/manul"
	"github.com/mhbvr/manul/db/bolt"
	"github.com/mhbvr/manul/db/filetree"
	"github.com/mhbvr/manul/db/pebble"
	"golang.org/x/image/draw"
)

func main() {
	var (
		dbType    = flag.String("type", "filetree", "Database type: filetree, bolt, or pebble")
		dbPath    = flag.String("db", "", "Database path (directory for filetree, file for bolt/pebble)")
		srcDir    = flag.String("src", "", "Source directory containing photo files")
		batchSize = flag.Int("batch-size", 100, "Number of photos to process in each transaction")
		scale     = flag.Float64("scale", 1.0, "Image scaling factor (0.0 to 1.0, where 1.0 = no scaling)")
	)
	flag.Parse()

	if *srcDir == "" {
		log.Fatal("Source directory must be specified with -src flag")
	}
	
	if *dbPath == "" {
		log.Fatal("Database path must be specified with -db flag")
	}

	if *scale <= 0.0 || *scale > 1.0 {
		log.Fatal("Scale factor must be between 0.0 (exclusive) and 1.0 (inclusive)")
	}

	var writer manul.DBWriter
	var err error

	switch *dbType {
	case "filetree":
		writer, err = filetree.New(*dbPath)
	case "bolt":
		writer, err = bolt.New(*dbPath)
	case "pebble":
		writer, err = pebble.New(*dbPath)
	default:
		log.Fatalf("Unknown database type: %s (must be 'filetree', 'bolt', or 'pebble')", *dbType)
	}

	if err != nil {
		log.Fatalf("Failed to create database writer: %v", err)
	}
	defer writer.Close()

	fmt.Printf("Creating %s database at: %s\n", *dbType, *dbPath)
	fmt.Printf("Scanning directory: %s\n", *srcDir)
	if *scale < 1.0 {
		fmt.Printf("Image scaling enabled: %.2f\n", *scale)
	}

	var totalFiles, skippedFiles int
	var filePaths []string

	// Single scan: collect file paths and count files
	err = filepath.Walk(*srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		totalFiles++
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
		log.Fatalf("Failed to scan source directory: %v", err)
	}

	processedFiles := 0
	fmt.Printf("Found %d files total, %d will be processed, %d skipped\n", totalFiles, len(filePaths), skippedFiles)
	fmt.Printf("Using batch size: %d\n", *batchSize)

	totalBatches := (len(filePaths) + *batchSize - 1) / *batchSize

	// Process files in batches
	for i := 0; i < len(filePaths); i += *batchSize {
		end := i + *batchSize
		if end > len(filePaths) {
			end = len(filePaths)
		}

		batchPaths := filePaths[i:end]
		batchNum := (i / *batchSize) + 1

		fmt.Printf("Processing batch %d/%d (%d photos)\n", batchNum, totalBatches, len(batchPaths))

		// Read and process this batch
		var batch []manul.PhotoItem
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

			// Scale the image if needed
			if *scale < 1.0 {
				scaledData, err := scaleImage(photoData, *scale)
				if err != nil {
					log.Fatalf("Failed to scale photo file %s: %v", path, err)
				}
				photoData = scaledData
			}

			batch = append(batch, manul.PhotoItem{
				CatID:     catID,
				PhotoID:   photoID,
				FilePath:  path,
				PhotoData: photoData,
			})

			fmt.Printf("  Added photo: cat_id=%d, photo_id=%d, size=%d bytes\n",
				catID, photoID, len(photoData))
		}

		fmt.Printf("Writing batch to DB %d/%d (%d photos)\n", batchNum, totalBatches, len(batch))
		if err := writer.AddPhotosBatch(batch); err != nil {
			log.Fatalf("Failed to process batch %d: %v", batchNum, err)
		}

		processedFiles += len(batch)
	}

	fmt.Printf("\nDatabase build completed successfully:\n")
	fmt.Printf("  Database type: %s\n", *dbType)
	fmt.Printf("  Database path: %s\n", *dbPath)
	fmt.Printf("  Total files found: %d\n", totalFiles)
	fmt.Printf("  Files processed: %d\n", processedFiles)
	fmt.Printf("  Files skipped: %d\n", skippedFiles)

	// Show database size/info
	switch *dbType {
	case "filetree":
		fmt.Printf("  Database created in directory: %s\n", *dbPath)
	case "bolt", "pebble":
		if stat, err := os.Stat(*dbPath); err == nil {
			fmt.Printf("  Database size: %d bytes\n", stat.Size())
		}
	}
}

func GetIDs(filename string) (catID, photoID uint64, ok bool) {
	var cat, photo uint64
	n, err := fmt.Sscanf(strings.ToLower(filename), "%d_%d.jpg", &cat, &photo)
	if err != nil || n != 2 {
		return 0, 0, false
	}
	return cat, photo, true
}

// scaleImage scales an image by the given factor using bilinear interpolation
func scaleImage(photoData []byte, scaleFactor float64) ([]byte, error) {
	if scaleFactor == 1.0 {
		return photoData, nil
	}

	// Decode the JPEG image
	img, err := jpeg.Decode(bytes.NewReader(photoData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// Calculate new dimensions
	bounds := img.Bounds()
	newWidth := int(float64(bounds.Dx()) * scaleFactor)
	newHeight := int(float64(bounds.Dy()) * scaleFactor)

	// Create a new image with the scaled dimensions
	scaledImg := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Use bilinear interpolation for scaling
	draw.BiLinear.Scale(scaledImg, scaledImg.Bounds(), img, bounds, draw.Over, nil)

	// Encode the scaled image back to JPEG with default quality
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaledImg, nil); err != nil {
		return nil, fmt.Errorf("failed to encode scaled image: %w", err)
	}

	return buf.Bytes(), nil
}