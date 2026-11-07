# Cell daemon image for local cluster smoke (compose).
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/celld ./cmd/celld \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cell ./cmd/cell \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cell-egress ./cmd/cell-egress

# Reuse the golang image as runtime so we do not need a second registry pull
# (debian:bookworm-slim often fails behind flaky Docker Hub / IPv6).
FROM golang:1.23-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends curl \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/celld /out/cell /out/cell-egress /usr/local/bin/
COPY environments /app/environments
ENV CELL_RUNTIME=host
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["celld"]
