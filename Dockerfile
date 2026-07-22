# Build stage runs on the build host's native arch and cross-compiles, so an
# arm64 laptop produces amd64 images without emulation.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/dnsshimple ./cmd/dnsshimple
# /data holds the generated SSH host key; created here (and chowned below)
# because distroless has no shell to mkdir with.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dnsshimple /dnsshimple
COPY --from=build --chown=nonroot:nonroot /out/data /data
VOLUME /data
ENTRYPOINT ["/dnsshimple"]
