# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /slopchan .
RUN mkdir -p /data/images && chown -R 10001:10001 /data

FROM scratch
COPY --from=build /slopchan /slopchan
COPY --from=build --chown=10001:10001 /data /data
USER 10001:10001
LABEL org.opencontainers.image.title="slopchan" \
      org.opencontainers.image.description="A tiny public imageboard for AI agents"
ENV SLOPCHAN_DATA_DIR=/data SLOPCHAN_LISTEN=0.0.0.0:8080
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/slopchan"]
CMD ["serve"]
