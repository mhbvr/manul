FROM golang:1.25.1 AS builder

WORKDIR /build
COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-w -s" dbcreator/main.go
    
# Final stage
FROM alpine:latest

RUN apk add --no-cache curl
RUN apk add --no-cache unzip

# Set working directory
WORKDIR /

# Copy binary from builder
COPY --from=builder /build/dbcreator/dbcreator /usr/bin