# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/pudding-relay ./cmd/pudding-relay

RUN mkdir -p /out/licenses && cp "$(go list -m -f '{{.Dir}}' github.com/coder/websocket)/LICENSE.txt" /out/licenses/websocket-LICENSE.txt

FROM alpine:3.23 AS server
RUN mkdir /data && chown 65532:65532 /data
COPY --from=build /out/pudding-relay /usr/local/bin/pudding-relay
COPY LICENSE /usr/share/doc/pudding-relay/LICENSE
COPY --from=build /out/licenses /usr/share/doc/pudding-relay/
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=3s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["pudding-relay"]
CMD ["--listen=0.0.0.0:8080", "--data-file=/data/registrations.json"]

# The public distribution includes the same compiled UI used by Pudding Desktop.
# Supply its binary assets as a named build context; private source is not copied.
FROM server AS distribution
COPY --from=browser / /assets/
ENV PUDDING_RELAY_ASSETS_DIR=/assets
