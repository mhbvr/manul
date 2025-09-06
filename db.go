package manul

// DBWriter provides an abstract interface for writing cat photo databases.
// Different implementations can store data in different formats (file tree vs single bbolt file).
type DBWriter interface {
	// AddPhoto adds a single photo to the database
	AddPhoto(catID, photoID uint64, photoData []byte) error
	
	// AddPhotosBatch adds multiple photos in a single transaction for better performance
	AddPhotosBatch(photos []PhotoItem) error
	
	// Close closes the database and releases resources
	Close() error
}

// DBReader provides an abstract interface for reading cat photo databases.
// Different implementations can read data from different formats (file tree vs single bbolt file).
type DBReader interface {
	// GetAllCatIDs returns all unique cat IDs in the database
	GetAllCatIDs() ([]uint64, error)
	
	// GetPhotoIDs returns all photo IDs for a specific cat
	GetPhotoIDs(catID uint64) ([]uint64, error)
	
	// GetPhotoData retrieves photo binary data by cat ID and photo ID
	GetPhotoData(catID, photoID uint64) ([]byte, error)
	
	// Close closes the database and releases resources
	Close() error
}

// PhotoItem represents a photo with its metadata and binary data
type PhotoItem struct {
	CatID     uint64
	PhotoID   uint64
	FilePath  string
	PhotoData []byte
}