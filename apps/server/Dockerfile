# Start from the official Golang image as a build stage
FROM golang:1.24.3-alpine AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source code into the container
COPY . .

# Build the Go app
RUN go build -o main ./src

# Start a new stage from scratch
FROM alpine:latest

WORKDIR /app

# Copy the pre-built binary file from the previous stage
COPY --from=builder /app/main .

# Expose port 8034 to the outside world
EXPOSE 8034

# Command to run the executable
CMD ["./main"]
