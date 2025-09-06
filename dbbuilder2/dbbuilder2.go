package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	bolt "go.etcd.io/bbolt"
)

const (
	metaBucket  = "meta"
	photoBucket = "photos"
)

type DBBuilder2 struct {
	dbPath string
	db     *bolt.DB
}

func New(dbPath string) (*DBBuilder2, error) {
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

	return &DBBuilder2{
		dbPath: dbPath,
		db:     db,
	}, nil
}

func (d *DBBuilder2) Close() error {
	return d.db.Close()
}

func (d *DBBuilder2) generateKey(catID, photoID uint64) []byte {
	key := make([]byte, 16)
	binary.BigEndian.PutUint64(key[:8], catID)
	binary.BigEndian.PutUint64(key[8:], photoID)
	return key
}

func (d *DBBuilder2) AddPhoto(catID, photoID uint64, photoData []byte) error {
	key := d.generateKey(catID, photoID)

	return d.db.Update(func(tx *bolt.Tx) error {
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

type PhotoItem struct {
	CatID     uint64
	PhotoID   uint64
	FilePath  string
	PhotoData []byte
}

func (d *DBBuilder2) AddPhotosBatch(photos []PhotoItem) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		metaBucket := tx.Bucket([]byte(metaBucket))
		photoBucket := tx.Bucket([]byte(photoBucket))

		for _, photo := range photos {
			key := d.generateKey(photo.CatID, photo.PhotoID)

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

func (d *DBBuilder2) AddPhotoFromFile(catID, photoID uint64, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open photo file: %w", err)
	}
	defer file.Close()

	photoData, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read photo file: %w", err)
	}

	if err := d.AddPhoto(catID, photoID, photoData); err != nil {
		return err
	}

	fmt.Printf("Added photo: cat_id=%d, photo_id=%d, size=%d bytes\n", catID, photoID, len(photoData))
	return nil
}
