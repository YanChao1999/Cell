# Dedicated allowlist egress sidecar — does not depend on the Cell app image.
# Build from repo root:
#   CGO_ENABLED=0 go build -o bin/cell-egress ./cmd/cell-egress
#   docker build -f docker/cell-egress/Dockerfile -t cell-egress:local .
#
# Or one-shot (needs Docker buildkit + network for go modules):
#   docker build -f docker/cell-egress/Dockerfile --target runtime -t cell-egress:local .

FROM public.ecr.aws/docker/library/golang:1.27-bookworm@sha256:dc0963f32053e4bc451d24d87e3892d42d01021b61f093e98eb95c0352bb84b0 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cell-egress ./cmd/cell-egress

FROM scratch AS runtime
COPY --from=build /out/cell-egress /cell-egress
EXPOSE 3128
ENTRYPOINT ["/cell-egress"]
