# DBBuilder2

A high-performance tool for building cat photo databases using a single bbolt file with optimized batch processing.

## Architecture

DBBuilder2 stores everything in a single bbolt database file with two buckets:

- **cat_photos bucket**: Metadata storage
  - Keys: 16-byte binary (cat_id + photo_id, big-endian)
  - Values: empty (reserved for future use)

- **photos bucket**: Photo data storage
  - Keys: 32-byte SHA256 hash of the cat_id,photo_id key
  - Values: raw photo binary data

## Usage

```bash
cd dbbuilder2
go run . -src=<source_directory> -db=<database_file> [options]
```

### Parameters

- `-src`: Source directory containing photo files (required)
- `-db`: Database file path (default: `./catdb2.db`)
- `-batch-size`: Number of photos to process per transaction (default: 100)

### Examples

```bash
# Basic usage
go run . -src=../testphotos -db=mydb.db

# Custom batch size for performance tuning
go run . -src=../photos -db=large.db -batch-size=500

# Small batch for memory-constrained systems
go run . -src=../photos -db=small.db -batch-size=10
```

## Performance Optimization

### Batch Processing
- Groups multiple photos into single database transactions
- Reduces disk I/O and improves performance significantly
- Configurable batch size based on available memory

### Streaming Processing
- Processes photos in batches without loading entire dataset into memory
- Memory usage: O(batch_size) instead of O(total_files)
- Scales to millions of photos regardless of available RAM

### Performance Comparison
- **Batch size 1**: N transactions for N photos
- **Batch size 100**: ⌈N/100⌉ transactions for N photos
- **Batch size 500**: ⌈N/500⌉ transactions for N photos

Larger batch sizes generally improve performance but use more memory.

## Photo Filename Format

Photos must follow the naming convention: `<cat_id>_<photo_id>.jpg`

Examples:
- `1_1.jpg` → cat_id=1, photo_id=1
- `2_5.jpg` → cat_id=2, photo_id=5
- `10_123.jpg` → cat_id=10, photo_id=123

Files not matching this pattern will be skipped.

## Output

Creates a single database file containing:
- All photo metadata in `cat_photos` bucket
- All photo binary data in `photos` bucket
- Efficient storage with SHA256 deduplication keys

## Database Inspection

Use bbolt CLI to inspect the database:

```bash
# Install bbolt CLI
go install go.etcd.io/bbolt/cmd/bbolt@latest

# List buckets
~/go/bin/bbolt buckets mydb.db

# List keys in metadata bucket
~/go/bin/bbolt keys mydb.db cat_photos

# List keys in photos bucket (SHA256 hashes)
~/go/bin/bbolt keys mydb.db photos

# Get photo data (example with hex key)
~/go/bin/bbolt get --parse-format=hex --format=ascii-encoded mydb.db photos 532deabf88729cb43995ab5a9cd49bf9b90a079904dc0645ecda9e47ce7345a9

# Show database statistics
~/go/bin/bbolt stats mydb.db
```

## Advantages over DBBuilder

1. **Single File**: Everything in one database file vs directory structure
2. **Better Performance**: Batch processing with configurable transaction sizes
3. **Memory Efficient**: Streaming processing, constant memory usage
4. **Scalability**: Handles large datasets without memory constraints
5. **Atomic Operations**: Each batch is atomic (all succeed or all fail)

## Memory Usage

- **DBBuilder**: O(total_file_size) - loads all photos into memory
- **DBBuilder2**: O(batch_size × avg_file_size) - constant memory usage

For 10,000 photos of 1MB each:
- **DBBuilder**: ~10GB RAM required
- **DBBuilder2**: ~100MB RAM with batch-size=100

## Notes

- Recommended batch-size: 100-1000 depending on available memory
- Database file grows incrementally during processing
- Use `/tmp` for database location if main filesystem has bbolt compatibility issues
- Progress reporting shows batch-by-batch processing status