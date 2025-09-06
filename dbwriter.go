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

// PhotoItem represents a photo with its metadata and binary data
type PhotoItem struct {
	CatID     uint64
	PhotoID   uint64
	FilePath  string
	PhotoData []byte
}