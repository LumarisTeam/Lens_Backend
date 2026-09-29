# 中国大陆网络环境：强制使用 goproxy.cn 代理
export GOPROXY=https://goproxy.cn,direct
export GO111MODULE=on

APP_BIN := bin/server

.PHONY: all tidy build vet test test-race run clean

all: vet build

## 下载并整理依赖
tidy:
	go mod tidy

## 编译全部包
build:
	go build -o $(APP_BIN) ./cmd/server

## 静态检查
vet:
	go vet ./...

## 运行全部单元测试
test:
	go test ./...

## 运行全部单元测试（含竞态检测）
test-race:
	go test -race ./...

## 本地运行（需先启动本地 PostgreSQL 与 Redis，见 README 快速开始）
run:
	go run ./cmd/server

## 清理产物
clean:
	rm -rf bin
