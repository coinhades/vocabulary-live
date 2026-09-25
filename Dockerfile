FROM node:24.14.0-bookworm-slim AS web
WORKDIR /source/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.27.1-bookworm AS server
WORKDIR /source/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

FROM scratch
COPY --from=server /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=server /server /app
COPY --from=web /source/frontend/dist /web
USER 10001:10001
ENV HTTP_ADDR=0.0.0.0:8080 STATIC_DIR=/web
EXPOSE 8080
ENTRYPOINT ["/app"]
