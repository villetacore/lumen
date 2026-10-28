# Образ lumend: один статический бинарник со вшитым веб-клиентом.
#
#   docker build -t lumen .
#   docker run -p 8787:8787 -v lumen-vault:/vault -e LUMEN_TOKEN=… lumen
#
# Внутри контейнера lumend слушает 0.0.0.0:8787, vault — /vault (том).
# Без LUMEN_TOKEN или OIDC сервер открыт всем, кто достучится до порта.

# --- веб-клиент ---
FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- сервер ---
FROM golang:1.26-alpine AS server
ARG VERSION=0.1.0-dev
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/web/dist/ ./internal/webui/dist/
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/lumend ./cmd/lumend \
 && mkdir /out/vault

# --- запуск ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/lumend /usr/local/bin/lumend
# Пустой /vault, принадлежащий nonroot: новый том Docker наследует владельца.
COPY --from=server --chown=nonroot:nonroot /out/vault /vault
ENV LUMEN_ADDR=0.0.0.0:8787 \
    LUMEN_VAULT=/vault
VOLUME /vault
EXPOSE 8787
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/lumend"]
