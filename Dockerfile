# Build stage
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/chatops-cli ./cmd/cli

# Runtime stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/chatops-cli /usr/local/bin/chatops-cli
ENTRYPOINT ["/usr/local/bin/chatops-cli"]
