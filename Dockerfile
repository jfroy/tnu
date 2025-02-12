FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.24-alpine AS builder

ARG TARGETARCH \
    TARGETOS \
    TARGETPLATFORM \
    TARGETVARIANT=""
ENV CGO_ENABLED=0 \
    GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT} \
    GOOS=${TARGETOS}

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY tnu.go ./
RUN go build -a -ldflags "-s -w" -trimpath -o tnu .

FROM alpine:latest
WORKDIR /
COPY --from=builder /app/tnu /usr/local/bin/tnu
ENTRYPOINT ["/usr/local/bin/tnu"]
