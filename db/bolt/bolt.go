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

func (w *BoltDB) parseKey(key []byte) (catID, photoID uint64) {
	if len(key) != 16 {
		return 0, 0
	}
	catID = binary.BigEndian.Uint64(key[:8])
	photoID = binary.BigEndian.Uint64(key[8:])
	return catID, photoID
}

func (w *BoltDB) GetAllCatIDs() ([]uint64, error) {
	catIdsMap := make(map[uint64]bool)

	err := w.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(metaBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", metaBucket)
		}

		cursor := bucket.Cursor()
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			catID, _ := w.parseKey(key)
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

func (w *BoltDB) GetPhotoIDs(catID uint64) ([]uint64, error) {
	var photoIds []uint64

	err := w.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(metaBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", metaBucket)
		}

		cursor := bucket.Cursor()
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			keyCatID, photoID := w.parseKey(key)
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

func (w *BoltDB) GetPhotoData(catID, photoID uint64) ([]byte, error) {
	key := w.generateKey(catID, photoID)
	var photoData []byte

	err := w.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(photoBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", photoBucket)
		}

		data := bucket.Get(key)
		if data == nil {
			return fmt.Errorf("photo with cat_id=%d, photo_id=%d not found in database", catID, photoID)
		}
		
		photoData = make([]byte, len(data))
		copy(photoData, data)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return photoData, nil
}

// NewReader creates a new BoltDB for reading (read-only mode)
func NewReader(dbPath string) (*BoltDB, error) {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to open bbolt database: %w", err)
	}

	return &BoltDB{
		db: db,
	}, nil
}