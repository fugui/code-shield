.PHONY: all build clean run start-notifier start-server

# 默认运行目标
all: build

# 完整打包构建
build:
	@echo "Building shield-server..."
	$(MAKE) -C shield-server build

# 清理构建产物
clean:
	@echo "Cleaning shield-server..."
	$(MAKE) -C shield-server clean
	@echo "Cleaning notifier..."
	rm -rf notifier/node_modules

# 安装 Node.js 依赖
install:
	@echo "Installing Notifier dependencies..."
	cd notifier && npm install

# 快捷启动整个系统 (当前终端会被阻塞)
run: build
	@echo "Starting Notifier..."
	cd notifier && npm start &
	@echo "Starting Shield Server..."
	$(MAKE) -C shield-server run

# 独立启动 Shield Server
start-server: build
	$(MAKE) -C shield-server run

# 独立启动 Notifier
start-notifier:
	cd notifier && npm start
