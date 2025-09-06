# DBBuilder

A tool for building cat photo databases using bbolt key-value storage with hierarchical file storage.

## Architecture

DBBuilder creates a database with the following structure:

- **meta**: bbolt database file containing metadata
  - Bucket: `cat_photos`
  - Keys: 16-byte binary (cat_id + photo_id, big-endian)
  - Values: empty (reserved for future use)

- **data/**: Hierarchical directory structure for photo files
  - Path format: `data/xx/filename`
  - Filename: SHA256 hash of the cat_id,photo_id key (hex format)
  - xx: First 2 characters of filename

## Usage

```bash
cd dbbuilder
go run . -src=<source_directory> -db=<database_directory>
```

### Parameters

- `-src`: Source directory containing photo files (required)
- `-db`: Database directory path (default: `./catdb`)

### Example

```bash
go run . -src=../testphotos -db=../mydb
```

## Photo Filename Format

Photos must follow the naming convention: `<cat_id>_<photo_id>.jpg`

Examples:
- `1_1.jpg` → cat_id=1, photo_id=1
- `2_5.jpg` → cat_id=2, photo_id=5
- `10_123.jpg` → cat_id=10, photo_id=123

Files not matching this pattern will be skipped.

## Output

The tool creates:

1. **Database directory** with:
   - `meta` file (bbolt database)
   - `data/` directory tree with photo files

2. **Progress logging** showing:
   - Files processed/skipped
   - Photo storage locations
   - Final statistics

## Example Output Structure

```
mydb/
├── meta                    # bbolt database
└── data/
    ├── 53/
    │   └── 532deabf...  # SHA256 filename
    ├── 8c/
    │   └── 8c7654ec...
    └── ...
```

## Database Inspection

Use bbolt CLI to inspect the database:

```bash
# Install bbolt CLI
go install go.etcd.io/bbolt/cmd/bbolt@latest

# List buckets
~/go/bin/bbolt buckets mydb/meta

# List keys
~/go/bin/bbolt keys mydb/meta cat_photos

# Show statistics
~/go/bin/bbolt stats mydb/meta
```

## Notes

- The tool processes files sequentially
- Database directory is created if it doesn't exist
- Existing photos are overwritten without warning