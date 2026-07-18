FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go.mod and go.sum and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the kiwid and kiwidaemon binaries
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kiwid ./cmd/kiwid
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kiwidaemon ./cmd/kiwidaemon

# Final stage
FROM golang:1.25-alpine

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

# Copy the binaries from the builder stage
COPY --from=builder /app/kiwid .
COPY --from=builder /app/kiwidaemon .

# Expose port
EXPOSE 8080

ENTRYPOINT ["./kiwid"]
