# Build stage runs on the native BUILDPLATFORM and cross-compiles for the
# requested TARGETOS/TARGETARCH, so multi-arch builds never pay the cost of
# emulating the Go toolchain under QEMU.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /src

# Cache dependencies first for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

# Build. CGO is disabled so the binary is fully static.
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Runtime stage: distroless static gives CA certs and a nonroot user for free.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/server"]
