FROM golang:1.26.5-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dead-drop ./cmd/dead-drop
RUN GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o /out/dead-drop.wasm ./cmd/dead-drop-wasm
RUN cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" /out/wasm_exec.js

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
RUN addgroup -S dead-drop && adduser -S -G dead-drop -u 10001 dead-drop
WORKDIR /app
COPY --from=builder /out/dead-drop /usr/local/bin/dead-drop
COPY --from=builder /out/dead-drop.wasm /app/web/static/dead-drop.wasm
COPY --from=builder /out/wasm_exec.js /app/web/static/wasm_exec.js
COPY web/static/deaddrop.js web/static/ui.js web/static/skin.css web/static/mark.jpg /app/web/static/
USER 10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/dead-drop"]
CMD ["serve", "-addr", ":8080", "-static", "/app/web/static"]
