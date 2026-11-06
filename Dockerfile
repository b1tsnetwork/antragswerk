# Kompiliert nativ auf der Build-Plattform (schnell auch für arm64), Laufzeit-Image je Zielplattform.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /antragswerk ./cmd/antragswerk

FROM alpine:3.24
ARG VERSION=dev
LABEL org.opencontainers.image.title="Antragswerk" org.opencontainers.image.version="${VERSION}"
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 aw && mkdir /data && chown aw /data
ENV TZ=Europe/Berlin
USER aw
COPY --from=build /antragswerk /usr/local/bin/antragswerk
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["antragswerk", "-config", "/data/config.yaml"]
