# DBBuilder2

A tool for building cat photo databases using a single bbolt file with batch processing.

## Architecture

DBBuilder2 stores everything in a single bbolt database file with two buckets:

- **meta bucket**: Metadata storage
  - Keys: 16-byte binary (cat_id + photo_id, big-endian)
  - Values: empty (reserved for future use)

- **photos bucket**: Photo data storage
  - Keys: the same as in meta bucket
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

## Photo Filename Format

Photos must follow the naming convention: `<cat_id>_<photo_id>.jpg`

Examples:
- `1_1.jpg` → cat_id=1, photo_id=1
- `2_5.jpg` → cat_id=2, photo_id=5
- `10_123.jpg` → cat_id=10, photo_id=123

Files not matching this pattern will be skipped.

## Output

Creates a single database file containing:
- All photo metadata in `meta` bucket
- All photo binary data in `photos` bucket

## Database Inspection

Use bbolt CLI to inspect the database:

```bash
# Install bbolt CLI
go install go.etcd.io/bbolt/cmd/bbolt@latest

# List buckets
~/go/bin/bbolt buckets mydb.db

# List keys in metadata bucket
~/go/bin/bbolt keys mydb.db meta

# List keys in photos bucket (SHA256 hashes)
~/go/bin/bbolt keys mydb.db photos

# Show database statistics
~/go/bin/bbolt stats mydb.db
```

## Notes

- Recommended batch-size: 100-1000 depending on available memory