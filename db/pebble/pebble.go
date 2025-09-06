package pebble

import (
	"encoding/binary"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/mhbvr/manul"
)

const (
	metaPrefix  = "meta:"
	photoPrefix = "photo:"
)

// PebbleDB implements DBWriter and DBReader interfaces using Pebble key-value storage
type PebbleDB struct {
	db *pebble.DB
}

// New creates a new PebbleDB for writing
func New(dbPath string) (*PebbleDB, error) {
	db, err := pebble.Open(dbPath, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble database: %w", err)
	}

	return &PebbleDB{
		db: db,
	}, nil
}

// NewReader creates a new PebbleDB for reading (read-only mode)
func NewReader(dbPath string) (*PebbleDB, error) {
	opts := &pebble.Options{
		ReadOnly: true,
	}
	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble database: %w", err)
	}

	return &PebbleDB{
		db: db,
	}, nil
}

func (p *PebbleDB) Close() error {
	return p.db.Close()
}

func (p *PebbleDB) generateKey(catID, photoID uint64) []byte {
	key := make([]byte, 16)
	binary.BigEndian.PutUint64(key[:8], catID)
	binary.BigEndian.PutUint64(key[8:], photoID)
	return key
}

func (p *PebbleDB) parseKey(key []byte) (catID, photoID uint64) {
	if len(key) != 16 {
		return 0, 0
	}
	catID = binary.BigEndian.Uint64(key[:8])
	photoID = binary.BigEndian.Uint64(key[8:])
	return catID, photoID
}

func (p *PebbleDB) metaKey(catID, photoID uint64) []byte {
	baseKey := p.generateKey(catID, photoID)
	prefixedKey := make([]byte, len(metaPrefix)+len(baseKey))
	copy(prefixedKey, []byte(metaPrefix))
	copy(prefixedKey[len(metaPrefix):], baseKey)
	return prefixedKey
}

func (p *PebbleDB) photoKey(catID, photoID uint64) []byte {
	baseKey := p.generateKey(catID, photoID)
	prefixedKey := make([]byte, len(photoPrefix)+len(baseKey))
	copy(prefixedKey, []byte(photoPrefix))
	copy(prefixedKey[len(photoPrefix):], baseKey)
	return prefixedKey
}

func (p *PebbleDB) AddPhoto(catID, photoID uint64, photoData []byte) error {
	batch := p.db.NewBatch()
	defer batch.Close()

	// Add metadata entry
	metaKey := p.metaKey(catID, photoID)
	if err := batch.Set(metaKey, []byte{}, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set metadata: %w", err)
	}

	// Add photo data
	photoKey := p.photoKey(catID, photoID)
	if err := batch.Set(photoKey, photoData, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set photo data: %w", err)
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	return nil
}

func (p *PebbleDB) AddPhotosBatch(photos []manul.PhotoItem) error {
	batch := p.db.NewBatch()
	defer batch.Close()

	for _, photo := range photos {
		// Add metadata entry
		metaKey := p.metaKey(photo.CatID, photo.PhotoID)
		if err := batch.Set(metaKey, []byte{}, pebble.NoSync); err != nil {
			return fmt.Errorf("failed to set metadata for cat_id=%d, photo_id=%d: %w", photo.CatID, photo.PhotoID, err)
		}

		// Add photo data
		photoKey := p.photoKey(photo.CatID, photo.PhotoID)
		if err := batch.Set(photoKey, photo.PhotoData, pebble.NoSync); err != nil {
			return fmt.Errorf("failed to set photo data for cat_id=%d, photo_id=%d: %w", photo.CatID, photo.PhotoID, err)
		}
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	return nil
}

func (p *PebbleDB) GetAllCatIDs() ([]uint64, error) {
	catIdsMap := make(map[uint64]bool)

	iter, err := p.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(metaPrefix),
		UpperBound: []byte(metaPrefix + "\xff"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		// Remove the prefix to get the original key
		if len(key) >= len(metaPrefix)+16 {
			baseKey := key[len(metaPrefix):]
			catID, _ := p.parseKey(baseKey)
			catIdsMap[catID] = true
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	var catIds []uint64
	for catID := range catIdsMap {
		catIds = append(catIds, catID)
	}

	return catIds, nil
}

func (p *PebbleDB) GetPhotoIDs(catID uint64) ([]uint64, error) {
	var photoIds []uint64

	iter, err := p.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(metaPrefix),
		UpperBound: []byte(metaPrefix + "\xff"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		// Remove the prefix to get the original key
		if len(key) >= len(metaPrefix)+16 {
			baseKey := key[len(metaPrefix):]
			keyCatID, photoID := p.parseKey(baseKey)
			if keyCatID == catID {
				photoIds = append(photoIds, photoID)
			}
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return photoIds, nil
}

func (p *PebbleDB) GetPhotoData(catID, photoID uint64) ([]byte, error) {
	photoKey := p.photoKey(catID, photoID)
	
	data, closer, err := p.db.Get(photoKey)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, fmt.Errorf("photo with cat_id=%d, photo_id=%d not found in database", catID, photoID)
		}
		return nil, fmt.Errorf("failed to get photo data: %w", err)
	}
	defer closer.Close()

	// Copy the data since it's only valid until closer.Close()
	photoData := make([]byte, len(data))
	copy(photoData, data)
	
	return photoData, nil
}