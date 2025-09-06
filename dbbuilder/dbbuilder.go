package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

const (
	metaBucket = "cat_photos"
)

type DBBuilder struct {
	dbPath   string
	dataPath string
	db       *bolt.DB
}

func New(dbDir string) (*DBBuilder, error) {
	metaPath := filepath.Join(dbDir, "meta")
	dataPath := filepath.Join(dbDir, "data")

	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	if err := os.MkdirAll(dataPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := bolt.Open(metaPath, 0600, &bolt.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open bbolt database: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(metaBucket))
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	return &DBBuilder{
		dbPath:   metaPath,
		dataPath: dataPath,
		db:       db,
	}, nil
}

func (d *DBBuilder) Close() error {
	return d.db.Close()
}

func (d *DBBuilder) generateKey(catID, photoID uint64) []byte {
	key := make([]byte, 16)
	binary.BigEndian.PutUint64(key[:8], catID)
	binary.BigEndian.PutUint64(key[8:], photoID)
	return key
}

func (d *DBBuilder) generateFilename(catID, photoID uint64) string {
	key := d.generateKey(catID, photoID)
	hash := sha256.Sum256(key)
	return fmt.Sprintf("%x", hash)
}

func (d *DBBuilder) getPhotoPath(catID, photoID uint64) string {
	filename := d.generateFilename(catID, photoID)

	xx := filename[:2]

	dir := filepath.Join(d.dataPath, xx)
	return filepath.Join(dir, filename)
}

func (d *DBBuilder) AddPhoto(catID, photoID uint64, photoData []byte) error {
	key := d.generateKey(catID, photoID)

	err := d.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(metaBucket))
		return bucket.Put(key, []byte{})
	})
	if err != nil {
		return fmt.Errorf("failed to update meta database: %w", err)
	}

	photoPath := d.getPhotoPath(catID, photoID)

	if err := os.MkdirAll(filepath.Dir(photoPath), 0755); err != nil {
		return fmt.Errorf("failed to create photo directory: %w", err)
	}

	if err := os.WriteFile(photoPath, photoData, 0644); err != nil {
		return fmt.Errorf("failed to write photo file: %w", err)
	}

	fmt.Printf("Added photo: cat_id=%d, photo_id=%d, path=%s\n", catID, photoID, photoPath)
	return nil
}

func (d *DBBuilder) AddPhotoFromFile(catID, photoID uint64, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open photo file: %w", err)
	}
	defer file.Close()

	photoData, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read photo file: %w", err)
	}

	return d.AddPhoto(catID, photoID, photoData)
}
