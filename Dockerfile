FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.23-alpine AS builder

ARG TARGETARCH \
    TARGETOS \
    TARGETPLATFORM \
    TARGETVARIANT=""
ENV CGO_ENABLED=0 \
    GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT} \
    GOOS=${TARGETOS}

RUN go build -a -ldflags "-s -w" -trimpath -o tnu .

FROM alpine:latest
RUN apk add --no-cache smartmontools
COPY --from=builder /tnu /usr/local/bin/tnu
ENTRYPOINT ["/usr/local/bin/tnu"]
