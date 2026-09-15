# Backend container. Two stages: the Go toolchain builds a static binary, the
# runtime image holds nothing but that binary and the CA certificates it
# needs to reach FCM and a TLS-protected database.
#
# The build context is the repository root, not backend/: the binary embeds
# the dashboard from web/, a sibling module (backend/go.mod replaces it with
# ../web). From the repository root:
#
#   docker build -f backend/Dockerfile -t ssh-sentinel-backend .
#   docker build -f backend/Dockerfile --build-arg VERSION=1.2.0 -t ssh-sentinel-backend:1.2.0 .
#
# The root .dockerignore keeps everything but backend/ and web/ out of the context.

ARG VERSION=dev

FROM golang:1.22-alpine AS build
ARG VERSION
WORKDIR /src

# Dependencies first, so that this layer is cached until a go.mod changes.
COPY backend/go.mod backend/go.sum backend/
COPY web/go.mod web/
RUN cd backend && go mod download

COPY backend/ backend/
COPY web/ web/
# CGO off and -trimpath give a static, reproducible binary that runs on the
# distroless image below (no libc there). -s -w drop the debug symbols.
RUN cd backend && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/server ./cmd/server

# distroless/static: no shell, no package manager, CA certificates included,
# runs as a non-root user. The HEALTHCHECK uses the binary itself because
# there is no curl here.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/server", "healthcheck"]
ENTRYPOINT ["/server"]
CMD ["serve"]
