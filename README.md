# Cat Photos gRPC Service

A simple gRPC service for managing cat photos.

## Build

Regenerate protobuf

```bash
protoc --go_out=paths=source_relative:.  --go-grpc_out=paths=source_relative:. cat_photos.proto
```

```bash
go mod download
```

## Run Server

```bash
cd server
go run . -host=localhost -port=8081
```

## Run Client

```bash
cd client

# List all cats
go run . -list-cats

# List photos for cat ID 1
go run . -list-photos=1

# Get photo and save to file
go run . -cat-id=1 -photo-id=1 -output=photo.dat
```

## API

- `ListCats()` - returns all cat IDs
- `ListPhotos(cat_id)` - returns photo IDs for a cat
- `GetPhoto(cat_id, photo_id)` - returns photo binary data
