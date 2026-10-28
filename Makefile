.PHONY: help server web app test check image build fmt clean

VAULT   ?= ./vault-example
ADDR    ?= 127.0.0.1:8787
VERSION ?= 0.1.0-dev
UI_DIST := server/internal/webui/dist
IMAGE   ?= lumen:dev

help:
	@echo "make app      — собрать один бинарник dist/lumend со вшитым веб-интерфейсом"
	@echo "make desktop  — собрать десктопный клиент (Flutter + Windows)"
	@echo "make server   — запустить lumend на $(ADDR) с vault $(VAULT)"
	@echo "make web      — запустить веб-клиент в режиме разработки"
	@echo "make test     — тесты Go и Rust"
	@echo "make check    — проверки как в CI: Go (fmt, vet, test) и веб (typecheck, test)"
	@echo "make image    — собрать контейнер $(IMAGE)"
	@echo "make fmt      — форматирование"
	@echo "make clean    — убрать артефакты сборки"

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
	cd server && go build -o ../dist/lumend ./cmd/lumend
	cd web && npm run build

fmt:
	cd server && gofmt -w .
	cd desktop/core && cargo fmt

clean:
	rm -rf dist web/dist $(UI_DIST)
	mkdir -p $(UI_DIST)
	printf '%s\n' 'Сюда make app копирует собранный веб-клиент из web/dist.' > $(UI_DIST)/.gitkeep
