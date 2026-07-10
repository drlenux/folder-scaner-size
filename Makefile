APP      := folder-size
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST     := dist
LDFLAGS  := -s -w -X main.version=$(VERSION)

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
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(APP) .

## Кросс-сборка под все платформы → dist/
build-all: clean
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		out="$(DIST)/$(APP)-$$os-$$arch$$ext"; \
		echo "→ $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags "$(LDFLAGS)" -o "$$out" . || exit 1; \
	done
	@echo "Готово: $(DIST)/"
	@ls -lh $(DIST)

clean:
	rm -rf $(DIST) $(APP) $(APP).exe
