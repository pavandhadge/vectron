# Stage 1: Build the Go binary
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy the Go module files
COPY go.mod ./
COPY go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/placementdriver ./cmd/placementdriver

# Stage 2: Create the final image
FROM gcr.io/distroless/static-debian11

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/placementdriver .

# Expose the gRPC and Raft ports
EXPOSE 6001 7001

# Run the binary
ENTRYPOINT ["/app/placementdriver"]
