# 中国大陆网络环境：强制使用 goproxy.cn 代理
export GOPROXY=https://goproxy.cn,direct
export GO111MODULE=on

APP_BIN := bin/server

.PHONY: all tidy build vet run clean

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

## 本地运行（需先 docker compose up -d 启动 PostgreSQL）
run:
	go run ./cmd/server

## 清理产物
clean:
	rm -rf bin
