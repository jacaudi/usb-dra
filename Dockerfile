FROM mirror.gcr.io/library/golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN go test ./... \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /rtlsdr-dra-kubeletplugin ./cmd/rtlsdr-dra-kubeletplugin

# The plugin is a static binary that only talks to sysfs, the kubelet sockets and
# the API server, so it needs nothing from userspace.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /rtlsdr-dra-kubeletplugin /usr/bin/rtlsdr-dra-kubeletplugin
ENTRYPOINT ["/usr/bin/rtlsdr-dra-kubeletplugin"]
