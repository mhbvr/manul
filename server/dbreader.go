package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

const (
	metaBucket = "cat_photos"
)

type DBReader struct {
	metaPath string
	dataPath string
	db       *bolt.DB
}

func NewDBReader(dbDir string) (*DBReader, error) {
	metaPath := filepath.Join(dbDir, "meta")
	dataPath := filepath.Join(dbDir, "data")

	db, err := bolt.Open(metaPath, 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &DBReader{
		metaPath: metaPath,
		dataPath: dataPath,
		db:       db,
	}, nil
}

func (r *DBReader) Close() error {
	return r.db.Close()
}

func (r *DBReader) generateKey(catID, photoID uint64) []byte {
	key := make([]byte, 16)
	binary.BigEndian.PutUint64(key[:8], catID)
	binary.BigEndian.PutUint64(key[8:], photoID)
	return key
}

func (r *DBReader) parseKey(key []byte) (catID, photoID uint64) {
	if len(key) != 16 {
		return 0, 0
	}
	catID = binary.BigEndian.Uint64(key[:8])
	photoID = binary.BigEndian.Uint64(key[8:])
	return catID, photoID
}

func (r *DBReader) generateFilename(catID, photoID uint64) string {
	key := r.generateKey(catID, photoID)
	hash := sha256.Sum256(key)
	return fmt.Sprintf("%x", hash)
}

func (r *DBReader) getPhotoPath(catID, photoID uint64) string {
	filename := r.generateFilename(catID, photoID)
	xx := filename[:2]
	dir := filepath.Join(r.dataPath, xx)
	return filepath.Join(dir, filename)
}

func (r *DBReader) GetAllCatIDs() ([]uint64, error) {
	catIdsMap := make(map[uint64]bool)

	err := r.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(metaBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", metaBucket)
		}

		cursor := bucket.Cursor()
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			catID, _ := r.parseKey(key)
			catIdsMap[catID] = true
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	var catIds []uint64
	for catID := range catIdsMap {
		catIds = append(catIds, catID)
	}

	return catIds, nil
}

func (r *DBReader) GetPhotoIDs(catID uint64) ([]uint64, error) {
	var photoIds []uint64

	err := r.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(metaBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", metaBucket)
		}

		cursor := bucket.Cursor()
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			keyCatID, photoID := r.parseKey(key)
			if keyCatID == catID {
				photoIds = append(photoIds, photoID)
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return photoIds, nil
}

func (r *DBReader) GetPhotoData(catID, photoID uint64) ([]byte, error) {
	key := r.generateKey(catID, photoID)

	err := r.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(metaBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", metaBucket)
		}

		value := bucket.Get(key)
		if value == nil {
			return fmt.Errorf("photo with cat_id=%d, photo_id=%d not found in database", catID, photoID)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	photoPath := r.getPhotoPath(catID, photoID)
	photoData, err := os.ReadFile(photoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read photo file %s: %w", photoPath, err)
	}

	return photoData, nil
}