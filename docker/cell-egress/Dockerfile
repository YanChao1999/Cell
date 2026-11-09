# Dedicated allowlist egress sidecar — does not depend on the Cell app image.
# Build from repo root:
#   CGO_ENABLED=0 go build -o bin/cell-egress ./cmd/cell-egress
#   docker build -f docker/cell-egress/Dockerfile -t cell-egress:local .
#
# Or one-shot (needs Docker buildkit + network for go modules):
#   docker build -f docker/cell-egress/Dockerfile --target runtime -t cell-egress:local .

FROM public.ecr.aws/docker/library/golang:1.26-bookworm@sha256:5de7c99eb78ca5e515a4c340235d0955ec5964a5071c492c15503d7c267da473 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cell-egress ./cmd/cell-egress

FROM scratch AS runtime
COPY --from=build /out/cell-egress /cell-egress
EXPOSE 3128
ENTRYPOINT ["/cell-egress"]
