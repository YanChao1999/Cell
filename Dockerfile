# Cell daemon image for local cluster smoke (compose).
# Prefer ECR mirror of Docker Hub library images when Hub IPv6 hangs.
# Digest-pinned (Dependabot can refresh). Tag 1.26-bookworm @ 2026-11-09.
FROM public.ecr.aws/docker/library/golang:1.27-bookworm@sha256:dc0963f32053e4bc451d24d87e3892d42d01021b61f093e98eb95c0352bb84b0 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/celld ./cmd/celld \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cell ./cmd/cell \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cell-egress ./cmd/cell-egress \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cell-guest-agent ./cmd/cell-guest-agent

# Reuse the golang image as runtime so we do not need a second registry pull
# (debian:bookworm-slim often fails behind flaky Docker Hub / IPv6).
FROM public.ecr.aws/docker/library/golang:1.27-bookworm@sha256:dc0963f32053e4bc451d24d87e3892d42d01021b61f093e98eb95c0352bb84b0
RUN apt-get update && apt-get install -y --no-install-recommends curl \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/celld /out/cell /out/cell-egress /usr/local/bin/
COPY environments /app/environments
ENV CELL_RUNTIME=host
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["celld"]
