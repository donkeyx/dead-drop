FROM golang:1.26.6-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p /out/web/static \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dead-drop ./cmd/dead-drop \
 && GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o /out/web/static/dead-drop.wasm ./cmd/dead-drop-wasm \
 && cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" /out/web/static/wasm_exec.js

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
 && addgroup -S dead-drop && adduser -S -G dead-drop -u 10001 dead-drop
WORKDIR /app
COPY --from=builder /out/dead-drop /usr/local/bin/dead-drop
COPY --from=builder /out/web/static /app/web/static
COPY web/static/deaddrop.js web/static/ui.js web/static/skin.css web/static/mark.jpg web/static/favicon.ico web/static/favicon.png web/static/apple-touch-icon.png /app/web/static/
USER 10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/dead-drop"]
CMD ["serve", "-addr", ":8080", "-static", "/app/web/static"]
