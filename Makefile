SHELL := /bin/bash
ENV := source scripts/env.sh

.DEFAULT_GOAL := help

.PHONY: help env deps web-install web-build typecheck build run dev-backend dev-web test vet fmt verify release timecheck clean clean-cache

help: ## 列出可用目标
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

env: ## 打印本项目使用的工具链缓存路径
	@IPHOTO_ENV_VERBOSE=1 bash -c 'source scripts/env.sh'

deps: ## 下载 Go 模块（缓存固定在 .cache/）
	$(ENV) && go mod download && go mod verify

web-install: ## 安装前端依赖（缓存固定在 .cache/npm）
	$(ENV) && cd web && npm install

web-build: ## 构建前端到 web/dist
	$(ENV) && cd web && npm run build && touch dist/.gitkeep

typecheck: ## 前端类型检查
	$(ENV) && cd web && npm run typecheck

build: web-build ## 构建单二进制到 bin/iphoto（内含前端）
	$(ENV) && go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o bin/iphoto ./cmd/app

run: build ## 构建并运行（library/runtime 落在项目根）
	IPHOTO_BASE_DIR=$(CURDIR) ./bin/iphoto

dev-backend: ## 开发模式跑后端（不打开浏览器，library 落在项目根）
	$(ENV) && IPHOTO_BASE_DIR=$(CURDIR) go run ./cmd/app -no-browser

dev-web: ## 开发模式跑 Vite（/api 代理到 18765）
	$(ENV) && cd web && npm run dev

test: ## 运行 Go 测试
	$(ENV) && go test ./...

vet: ## 运行 go vet
	$(ENV) && go vet ./...

fmt: ## 格式化 Go 代码（不触碰 .cache 内的模块缓存）
	$(ENV) && gofmt -l -w cmd internal web

verify: ## 一键验证开发环境（构建 + 启动 + 健康检查）
	$(ENV) && bash scripts/verify-env.sh

timecheck: ## 构建时间解析预检工具到 bin/iphoto-timecheck
	$(ENV) && go build -trimpath -o bin/iphoto-timecheck ./cmd/timecheck

release: web-build ## 交叉编译多平台单二进制到 dist/
	$(ENV) && mkdir -p dist
	$(ENV) && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/iphoto-darwin-arm64 ./cmd/app
	$(ENV) && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/iphoto-darwin-amd64 ./cmd/app
	$(ENV) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/iphoto-linux-amd64 ./cmd/app
	$(ENV) && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/iphoto-windows-amd64.exe ./cmd/app

win: web-build ## 构建 Windows 平台单二进制到 dist/
	$(ENV) && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/iphoto-windows-amd64.exe ./cmd/app

install-win: ## 安装程序到目标路径
	cp  dist/iphoto-windows-amd64.exe ~/Pictures/

clean: ## 删除构建产物
	rm -rf bin

clean-cache: ## 删除项目内工具链缓存（下次会重新下载）
	rm -rf .cache
