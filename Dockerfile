# Build on a specific image
FROM golang:latest AS builder

# Install build dependencies
RUN go install github.com/magefile/mage@latest

# Copy source code
WORKDIR /app
COPY . .

# Set as portable binary
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

# Build
RUN mage build

# Create a minimal image
FROM alpine:latest

# With only the portable binary
COPY --from=builder /app/dist/poepenai /usr/local/bin/poepenai

# And start it
EXPOSE 8080
CMD ["poepenai", "start"]