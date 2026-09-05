FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /slopchan . 
RUN mkdir -p /data/images && chown -R 10001:10001 /data

FROM scratch
COPY --from=build /slopchan /slopchan
COPY --from=build --chown=10001:10001 /data /data
USER 10001:10001
ENV SLOPCHAN_DATA_DIR=/data SLOPCHAN_LISTEN=0.0.0.0:8080
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/slopchan"]
CMD ["serve"]
