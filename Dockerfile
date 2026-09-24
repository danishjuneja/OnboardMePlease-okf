FROM node:24.21.0-bookworm-slim AS web-build
WORKDIR /source/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.27.1-bookworm AS go-build
WORKDIR /source
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=web-build /source/internal/webui/dist/ ./internal/webui/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/onboardmeplease ./cmd/onboardmeplease

FROM golang:1.27.1-bookworm
RUN groupadd --system onboard && useradd --system --gid onboard --home /app onboard
WORKDIR /app
COPY --from=go-build /out/onboardmeplease /app/onboardmeplease
RUN mkdir -p /app/data && chown onboard:onboard /app/data
USER onboard
ENV APP_DATA_DIR=/app/data APP_LISTEN_ADDR=0.0.0.0:8765 APP_CONTAINER_MODE=1 MODEL_MODE=strict_local
EXPOSE 8765
ENTRYPOINT ["/app/onboardmeplease"]
