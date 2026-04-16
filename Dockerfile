FROM golang:1.22-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/psh ./cmd/pipery

FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/psh /usr/local/bin/psh

RUN groupadd -r psh && useradd --no-log-init -r -g psh psh
USER psh

WORKDIR /workspace

ENTRYPOINT ["psh"]
CMD ["-h"]
