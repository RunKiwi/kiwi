FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Copy go.mod and go.sum and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the kiwid binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kiwid ./cmd/kiwid

# Final stage
FROM alpine:latest

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

# Copy the binary from the builder stage
COPY --from=builder /app/kiwid .

# Expose port
EXPOSE 8080

ENTRYPOINT ["./kiwid"]
