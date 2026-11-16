FROM mirror.gcr.io/library/golang:1.27-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN go test ./... \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /usb-dra-kubeletplugin ./cmd/usb-dra-kubeletplugin

# The plugin is a static binary that only talks to sysfs, the kubelet sockets and
# the API server, so it needs nothing from userspace.
#
# Deliberately NOT the :nonroot tag. A kubelet plugin has to create its socket under
# /var/lib/kubelet/plugins and write CDI specs under /var/run/cdi, both root-owned on
# the host. privileged: true does not change the UID, so a nonroot image fails at
# startup with "mkdir ...: permission denied".
FROM gcr.io/distroless/static-debian12:latest@sha256:a9fcaedd4c9b59e12dd65d954f0b5044f19b0647a8a3712e77205df9e7b102cd

COPY --from=build /usb-dra-kubeletplugin /usr/bin/usb-dra-kubeletplugin
ENTRYPOINT ["/usr/bin/usb-dra-kubeletplugin"]
