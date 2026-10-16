# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/packaging-ai ./cmd/server

FROM alpine:3.20
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder /out/packaging-ai /app/packaging-ai
COPY web /app/web
USER app
EXPOSE 8080
ENTRYPOINT ["/app/packaging-ai"]
