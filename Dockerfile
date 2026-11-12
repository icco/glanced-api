FROM golang:1.27-bookworm AS builder
WORKDIR /app
ENV GOOS=linux
ENV CGO_ENABLED=0
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -o /app/bin/glanced-api ./cmd/glanced-api

FROM debian:bookworm-slim
LABEL org.opencontainers.image.source=https://github.com/icco/glanced-api
LABEL org.opencontainers.image.description="Sync API for Glanced Analog cards."
LABEL org.opencontainers.image.licenses=MIT
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
RUN groupadd -r app && useradd -r -u 1001 -g app app
WORKDIR /app
COPY --from=builder --chown=app:app /app/bin/ /app/bin/
USER app
EXPOSE 8080
CMD ["/app/bin/glanced-api", "serve"]
