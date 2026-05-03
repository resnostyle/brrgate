##
## Build statically linked linux binaries without cgo deps (multi-arch friendly).
##

FROM golang:1.22.12-bookworm AS build

WORKDIR /src

ENV CGO_ENABLED=0 GOTOOLCHAIN=local

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
	go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 \
	go build -trimpath -ldflags="-s -w" \
	-o /out/autobrr-hook .

##
## Lightweight runtime surface with CA bundle for outbound HTTPS callers.
##

FROM alpine:3.20

LABEL org.opencontainers.image.title="autobrr-hook"

RUN apk add --no-cache ca-certificates && \
	addgroup -g 65532 svc && \
	adduser -D -u 65532 -G svc svc

WORKDIR /home/svc

COPY --from=build /out/autobrr-hook /usr/local/bin/autobrr-hook

USER svc:svc

ENV LISTEN_ADDR=:8080

EXPOSE 8080
EXPOSE 8091

ENTRYPOINT ["/usr/local/bin/autobrr-hook"]
