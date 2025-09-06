package filetree

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mhbvr/manul"
	bolt "go.etcd.io/bbolt"
)

const (
	metaBucket = "cat_photos"
	metaFile   = "meta"
	dataDir    = "data"
)

// FileTreeDB implements DBWriter interface using bbolt for metadata and filesystem for photos
type FileTreeDB struct {
	metaPath string
	dataPath string
	db       *bolt.DB
}

// New creates a new FileTreeDB
func New(dbDir string) (*FileTreeDB, error) {
	metaPath := filepath.Join(dbDir, metaFile)
	dataPath := filepath.Join(dbDir, dataDir)

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

	return &FileTreeDB{
		metaPath: metaPath,
		dataPath: dataPath,
		db:       db,
	}, nil
}

func (w *FileTreeDB) Close() error {
	return w.db.Close()
}

func (w *FileTreeDB) generateKey(catID, photoID uint64) []byte {
	key := make([]byte, 16)
	binary.BigEndian.PutUint64(key[:8], catID)
	binary.BigEndian.PutUint64(key[8:], photoID)
	return key
}

func (w *FileTreeDB) generateFilename(catID, photoID uint64) string {
	key := w.generateKey(catID, photoID)
	hash := sha256.Sum256(key)
	return fmt.Sprintf("%x", hash)
}

func (w *FileTreeDB) getPhotoPath(catID, photoID uint64) string {
	filename := w.generateFilename(catID, photoID)
	xx := filename[:2]
	dir := filepath.Join(w.dataPath, xx)
	return filepath.Join(dir, filename)
}

func (w *FileTreeDB) AddPhoto(catID, photoID uint64, photoData []byte) error {
	key := w.generateKey(catID, photoID)

	err := w.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(metaBucket))
		return bucket.Put(key, []byte{})
	})
	if err != nil {
		return fmt.Errorf("failed to update meta database: %w", err)
	}

	photoPath := w.getPhotoPath(catID, photoID)

	if err := os.MkdirAll(filepath.Dir(photoPath), 0755); err != nil {
		return fmt.Errorf("failed to create photo directory: %w", err)
	}

	if err := os.WriteFile(photoPath, photoData, 0644); err != nil {
		return fmt.Errorf("failed to write photo file: %w", err)
	}

	return nil
}

func (w *FileTreeDB) AddPhotosBatch(photos []manul.PhotoItem) error {
	// First update metadata in a single transaction
	err := w.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(metaBucket))
		for _, photo := range photos {
			key := w.generateKey(photo.CatID, photo.PhotoID)
			if err := bucket.Put(key, []byte{}); err != nil {
				return fmt.Errorf("failed to update meta for cat_id=%d, photo_id=%d: %w", photo.CatID, photo.PhotoID, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Then write all photo files
	for _, photo := range photos {
		photoPath := w.getPhotoPath(photo.CatID, photo.PhotoID)

		if err := os.MkdirAll(filepath.Dir(photoPath), 0755); err != nil {
			return fmt.Errorf("failed to create photo directory: %w", err)
		}

		if err := os.WriteFile(photoPath, photo.PhotoData, 0644); err != nil {
			return fmt.Errorf("failed to write photo file: %w", err)
		}
	}

	return nil
}