.PHONY: help dev dev-prod server web app test check image build fmt clean

VAULT   ?= ./vault-example
ADDR    ?= 127.0.0.1:8787
# Из git: на теге v1.2.3 — 1.2.3, между релизами — 1.2.3-4-gabcdef012, без тегов — 0.1.0-dev.
VERSION ?= $(or $(shell git describe --tags --match 'v[0-9]*' --dirty 2>/dev/null | sed 's/^v//'),0.1.0-dev)
UI_DIST := server/internal/webui/dist
IMAGE   ?= lumen:dev
# make dev работает с копией vault-example, чтобы не портить примеры в git.
DEV_VAULT  ?= .dev-vault
# make dev-prod: боевой сервер и токен к нему (файл или LUMEN_TOKEN).
PROD_URL   ?= https://lumen.velessoft.org
TOKEN_FILE ?= $(HOME)/.config/lumen/token

help:
	@echo "make dev      — lumend + веб-клиент с hot-reload: http://localhost:5173 (vault $(DEV_VAULT))"
	@echo "make dev-prod — веб-клиент с hot-reload поверх $(PROD_URL) (боевые данные!)"
	@echo "make app      — собрать один бинарник dist/lumend со вшитым веб-интерфейсом"
	@echo "make desktop  — собрать десктопный клиент (Flutter + Windows)"
	@echo "make server   — запустить lumend на $(ADDR) с vault $(VAULT)"
	@echo "make web      — запустить веб-клиент в режиме разработки"
	@echo "make test     — тесты Go и Rust"
	@echo "make check    — проверки как в CI: Go (fmt, vet, test) и веб (typecheck, test)"
	@echo "make image    — собрать контейнер $(IMAGE)"
	@echo "make fmt      — форматирование"
	@echo "make clean    — убрать артефакты сборки"

# Сервер и веб-клиент разом; Ctrl+C останавливает оба. Правки web/ видны
# сразу, правки server/ — после перезапуска make dev.
dev: web/node_modules
	@test -d $(DEV_VAULT) || cp -R vault-example $(DEV_VAULT)
	@trap 'kill 0' INT TERM EXIT; \
	  (cd server && go run ./cmd/lumend --vault ../$(DEV_VAULT) --addr $(ADDR) --ui=false) & \
	  (cd web && LUMEN_API=http://$(ADDR) npm run dev) & \
	  wait

# Локальный веб-клиент против боевого сервера: удобно править интерфейс на
# настоящих данных. Изменения уходят в боевой vault. Токен добавляет dev-сервер
# Next (web/src/proxy.dev.ts), в браузер он не попадает.
dev-prod: web/node_modules
	@token="$${LUMEN_TOKEN:-$$(cat $(TOKEN_FILE) 2>/dev/null)}"; \
	  test -n "$$token" || { echo "нет токена: LUMEN_TOKEN или $(TOKEN_FILE)"; exit 1; }; \
	  cd web && LUMEN_API=$(PROD_URL) LUMEN_TOKEN="$$token" npm run dev

web/node_modules: web/package-lock.json
	cd web && npm ci --no-audit --no-fund
	@touch $@

server:
	cd server && go run ./cmd/lumend --vault ../$(VAULT) --addr $(ADDR)

web:
	cd web && npm run dev

# Одно приложение: веб-клиент вшивается в бинарник через go:embed.
app:
	cd web && npm run build
	rm -rf $(UI_DIST)
	mkdir -p $(UI_DIST)
	cp -r web/dist/. $(UI_DIST)/
	printf '%s\n' 'Сюда make app копирует собранный веб-клиент из web/dist.' > $(UI_DIST)/.gitkeep
	cd server && go build -ldflags "-X main.version=$(VERSION)" -o ../dist/lumend ./cmd/lumend
	@echo "Готово: dist/lumend --vault $(VAULT) --open"

# Десктоп собирается через скрипт: он обходит битые записи PATH,
# из-за которых flutter_assemble падает на локализованной Windows.
# Подробности — desktop/app/README.md.
desktop:
	powershell -NoProfile -File ./scripts/desktop.ps1

test:
	cd server && go test ./...
	cd desktop/core && cargo test
	cd web && npx tsc -b --noEmit
	powershell -NoProfile -File ./scripts/desktop.ps1 -Test

# Проверки открытого CI (.github/workflows/ci.yml) без desktop.
check:
	cd server && test -z "$$(gofmt -l .)" && go vet ./... && go test ./...
	cd web && npm run typecheck && npm run test --if-present

image:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

build:
	cd server && go build -ldflags "-X main.version=$(VERSION)" -o ../dist/lumend ./cmd/lumend
	cd web && npm run build

fmt:
	cd server && gofmt -w .
	cd desktop/core && cargo fmt

clean:
	rm -rf dist web/dist web/.next $(UI_DIST)
	mkdir -p $(UI_DIST)
	printf '%s\n' 'Сюда make app копирует собранный веб-клиент из web/dist.' > $(UI_DIST)/.gitkeep
