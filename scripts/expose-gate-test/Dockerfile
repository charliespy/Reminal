# The PIN gate in front of a forwarded port, on the real Worker (wrangler
# dev, local) with a real `reminal expose` (run.sh). The Worker from before
# (the "old" build context) issues a cookie first, then this one takes over.
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/ultraviolet/go.mod third_party/ultraviolet/go.mod
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-buildvcs=false go build -o /out/reminal ./cmd/reminal

FROM node:22-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends python3 curl && rm -rf /var/lib/apt/lists/*
COPY cloudflare/package.json cloudflare/package-lock.json /w/cloudflare/
RUN cd /w/cloudflare && npm ci --silent
COPY cloudflare /w/cloudflare
COPY cloudflare/src /w/new-src
COPY --from=old cloudflare/src /w/old-src
COPY internal/client/web /w/internal/client/web
COPY --from=build /out/reminal /usr/local/bin/reminal
COPY scripts/tunnel-cookie-test/backend.py scripts/expose-gate-test/test.sh /t/
CMD ["sh", "/t/test.sh"]
