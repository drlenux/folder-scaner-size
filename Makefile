APP      := folder-size
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST     := dist
LDFLAGS  := -s -w -X main.version=$(VERSION)

# Локальный кеш — не зависит от ~/Library/Caches/go-build (часто ломается от root/sandbox).
export GOCACHE := $(CURDIR)/.gocache
export CGO_ENABLED := 0

# GOOS/GOARCH пары для релизной сборки
PLATFORMS := \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/arm64 \
	windows/amd64 \
	windows/arm64

.PHONY: all build build-all clean

all: build

## Сборка под текущую ОС
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(APP) .

## Кросс-сборка под все платформы → dist/
build-all: clean
	@mkdir -p $(DIST) $(GOCACHE)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		out="$(DIST)/$(APP)-$$os-$$arch$$ext"; \
		echo "→ $$out"; \
		GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags "$(LDFLAGS)" -o "$$out" . || exit 1; \
	done
	@echo "Готово: $(DIST)/"
	@ls -lh $(DIST)

clean:
	rm -rf $(DIST) $(APP) $(APP).exe

## Починить системный go-build кеш, если он от root
fix-gocache:
	@echo "Нужен пароль sudo, чтобы сменить владельца ~/Library/Caches/go-build"
	sudo chown -R "$$(whoami):staff" "$$HOME/Library/Caches/go-build"
