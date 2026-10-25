FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/classroom-notify ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata su-exec wget \
	&& adduser -D -H -u 10001 app
COPY --from=build /out/classroom-notify /usr/local/bin/classroom-notify
COPY docker-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENV DATA_DIR=/data PORT=8080 TZ=Asia/Shanghai
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
	CMD wget -q -O- http://127.0.0.1:8080/health | grep -q '"ok":true' || exit 1
ENTRYPOINT ["/entrypoint.sh"]
