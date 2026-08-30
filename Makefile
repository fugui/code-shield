BINARY          := code-shield-server
FRONTEND_DIR    := frontend
DIST_DIR        := $(FRONTEND_DIR)/dist
NODE_MODULES    := $(FRONTEND_DIR)/node_modules

VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT          ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILDTIME       ?= $(shell date -u '+%Y-%m-%d %H:%M:%S')
LDFLAGS         := -X 'main.Version=$(VERSION)' -X 'main.CommitID=$(COMMIT)' -X 'main.BuildTime=$(BUILDTIME)'

COMMON_DIR      := ../code-common/frontend/src
COMMON_SRCS     := $(shell find $(COMMON_DIR) -type f 2>/dev/null)

# 自动收集前端与后端源码依赖
FRONTEND_SRCS   := $(shell find $(FRONTEND_DIR) -type f -not -path "*/node_modules/*" -not -path "*/dist/*" 2>/dev/null) $(COMMON_SRCS)
BACKEND_SRCS    := $(shell find . -type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" \) -not -path "*/$(FRONTEND_DIR)/*" -not -path "*/.git/*")

.PHONY: all build install frontend backend clean run test lint

# 默认运行目标
all: build

# 完整打包构建
build: $(BINARY)

# 依赖安装 (node_modules)
install: $(NODE_MODULES)

$(NODE_MODULES): $(FRONTEND_DIR)/package.json
	cd $(FRONTEND_DIR) && ( [ -d node_modules ] || npm install )
	@touch $(NODE_MODULES)

# 编译构建前端静态资产 (dist/)
frontend: $(DIST_DIR)

$(DIST_DIR): $(NODE_MODULES) $(FRONTEND_SRCS)
	cd $(FRONTEND_DIR) && npm run build
	@touch $(DIST_DIR)

# 编译后端可执行文件
backend: $(BINARY)

$(BINARY): $(BACKEND_SRCS) $(DIST_DIR)
	go mod download
	go build -ldflags "$(LDFLAGS)" -o $(BINARY)

# 清理构建产物
clean:
	rm -rf $(DIST_DIR) $(BINARY)

# 运行单元测试
test:
	go test ./...

# 快捷启动命令
run: build
	./$(BINARY)

# 执行代码风格与语法检查
lint: $(NODE_MODULES)
	@echo "Running linter..."
	cd $(FRONTEND_DIR) && npm run lint
