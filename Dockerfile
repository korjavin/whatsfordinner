FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/whatsfordinner ./cmd/bot

# Create a minimal image
FROM alpine:latest

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/whatsfordinner .

# Create data directory for BadgerDB
RUN mkdir -p /app/data

# Set the entrypoint
ENTRYPOINT ["/app/whatsfordinner"]
