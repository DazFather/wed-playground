FROM golang:1.26-bookworm AS builder

RUN git clone https://github.com/DazFather/Wednesday.git /src/Wednesday

WORKDIR /src/Wednesday

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /wed \
    ./cmd/wed


FROM debian:bookworm-slim

COPY --from=builder /wed /usr/local/bin/wed
COPY wed-settings.json /etc/wed-settings.json

RUN useradd --system --uid 10001 --no-create-home sandbox

USER 10001:10001

WORKDIR /workspace

