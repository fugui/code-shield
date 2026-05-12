.PHONY: all build clean frontend backend run

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILDTIME ?= $(shell date -u '+%Y-%m-%d %H:%M:%S')
LDFLAGS  := -X 'main.Version=$(VERSION)' -X 'main.CommitID=$(COMMIT)' -X 'main.BuildTime=$(BUILDTIME)'

# 默认运行目标
all: build

# 完整打包构建
build: frontend backend

# 清理构建产物
clean:
	rm -rf code-shield-server frontend/dist

# 独立编译前端
frontend:
	cd frontend && ( [ -d node_modules ] || npm install )
	cd frontend && npm run build

# 独立编译后端
backend:
	go mod download
	go build -ldflags "$(LDFLAGS)" -o code-shield-server

# 快捷启动命令
run: build
	./code-shield-server
