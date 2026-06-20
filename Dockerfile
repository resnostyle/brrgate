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
	-o /out/brrgate .

##
## Lightweight runtime surface with CA bundle for outbound HTTPS callers.
##

FROM alpine:3.24

LABEL org.opencontainers.image.title="brrgate"

RUN apk add --no-cache ca-certificates && \
	addgroup -g 65532 svc && \
	adduser -D -u 65532 -G svc svc

WORKDIR /home/svc

COPY --from=build /out/brrgate /usr/local/bin/brrgate

USER svc:svc

ENV LISTEN_ADDR=:8080

EXPOSE 8080
EXPOSE 8091

ENTRYPOINT ["/usr/local/bin/brrgate"]
