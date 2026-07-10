# Build stage runs on the native BUILDPLATFORM and cross-compiles for the
# requested TARGETOS/TARGETARCH, so multi-arch builds never pay the cost of
# emulating the Go toolchain under QEMU.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /src

# Cache dependencies first for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

# Build. CGO is disabled so the binary is fully static. VERSION/GIT_SHA/
# BUILD_DATE are stamped into internal/buildinfo via -ldflags -X; they default
# to the package's own "dev"/"none"/"unknown" so a plain `docker build` still
# works. The release workflow supplies the real values.
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG GIT_SHA=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags="-s -w \
      -X github.com/kenlasko/adguard-logcentral/internal/buildinfo.Version=${VERSION} \
      -X github.com/kenlasko/adguard-logcentral/internal/buildinfo.Commit=${GIT_SHA} \
      -X github.com/kenlasko/adguard-logcentral/internal/buildinfo.Date=${BUILD_DATE}" \
    -o /out/server ./cmd/server

# Runtime stage: distroless static gives CA certs and a nonroot user for free.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/server"]
