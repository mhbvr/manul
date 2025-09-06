package bolt

import (
	"encoding/binary"
	"fmt"

	"github.com/mhbvr/manul"
	bolt "go.etcd.io/bbolt"
)

const (
	metaBucket  = "meta"
	photoBucket = "photos"
)

// BoltDB implements DBWriter interface using single bbolt file for everything
type BoltDB struct {
	db *bolt.DB
}

// New creates a new BoltDB
func New(dbPath string) (*BoltDB, error) {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open bbolt database: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(metaBucket)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(photoBucket)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create buckets: %w", err)
	}

	return &BoltDB{
		db: db,
	}, nil
}

func (w *BoltDB) Close() error {
	return w.db.Close()
}

func (w *BoltDB) generateKey(catID, photoID uint64) []byte {
	key := make([]byte, 16)
	binary.BigEndian.PutUint64(key[:8], catID)
	binary.BigEndian.PutUint64(key[8:], photoID)
	return key
}

func (w *BoltDB) AddPhoto(catID, photoID uint64, photoData []byte) error {
	key := w.generateKey(catID, photoID)

	return w.db.Update(func(tx *bolt.Tx) error {
		metaBucket := tx.Bucket([]byte(metaBucket))
		if err := metaBucket.Put(key, []byte{}); err != nil {
			return fmt.Errorf("failed to update meta bucket: %w", err)
		}

		photoBucket := tx.Bucket([]byte(photoBucket))
		if err := photoBucket.Put(key, photoData); err != nil {
			return fmt.Errorf("failed to update photo bucket: %w", err)
		}

		return nil
	})
}

func (w *BoltDB) AddPhotosBatch(photos []manul.PhotoItem) error {
	return w.db.Update(func(tx *bolt.Tx) error {
		metaBucket := tx.Bucket([]byte(metaBucket))
		photoBucket := tx.Bucket([]byte(photoBucket))

		for _, photo := range photos {
			key := w.generateKey(photo.CatID, photo.PhotoID)

			if err := metaBucket.Put(key, []byte{}); err != nil {
				return fmt.Errorf("failed to update meta bucket for cat_id=%d, photo_id=%d: %w", photo.CatID, photo.PhotoID, err)
			}

			if err := photoBucket.Put(key, photo.PhotoData); err != nil {
				return fmt.Errorf("failed to update photo bucket for cat_id=%d, photo_id=%d: %w", photo.CatID, photo.PhotoID, err)
			}
		}

		return nil
	})
}