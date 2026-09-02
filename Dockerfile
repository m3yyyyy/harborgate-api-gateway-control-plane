# syntax=docker/dockerfile:1.7
FROM node:24-alpine AS web-build
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS go-build
ARG VERSION=dev
ARG COMMIT=none
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
  -o /out/harborgate ./cmd/harborgate

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata \
  && addgroup -S harborgate \
  && adduser -S -G harborgate -h /app harborgate \
  && mkdir -p /app/data /app/web \
  && chown -R harborgate:harborgate /app
WORKDIR /app
COPY --from=go-build /out/harborgate ./harborgate
COPY --from=web-build /src/web/dist ./web
ENV PORT=8080 \
    DATA_PATH=/app/data/harborgate.db \
    WEB_DIR=/app/web
EXPOSE 8080
USER harborgate
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/api/health || exit 1
ENTRYPOINT ["/app/harborgate"]
