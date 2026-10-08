FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git ca-certificates
COPY go.mod ./
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/server ./cmd/server/main.go
FROM alpine:3.20
WORKDIR /app
RUN apk --no-cache add ca-certificates curl
COPY --from=builder /app/bin/server /app/server
COPY --from=builder /app/migrations /app/migrations
EXPOSE 8080
ENTRYPOINT ["/app/server"]
