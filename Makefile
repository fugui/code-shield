.PHONY: all build clean run start-notifier start-server

# 默认运行目标
all: build

# 完整打包构建
build:
	@echo "Building shield-server..."
	$(MAKE) -C shield-server build
	@echo "Building notifier..."
	$(MAKE) -C notifier build

# 清理构建产物
clean:
	@echo "Cleaning shield-server..."
	$(MAKE) -C shield-server clean
	@echo "Cleaning notifier..."
	$(MAKE) -C notifier clean

# 安装依赖 (现在只有前端需要 npm)
install:
	@echo "Installing Frontend dependencies..."
	cd shield-server/frontend && ( [ -d node_modules ] || npm install )

# 快捷启动整个系统 (当前终端会被阻塞)
run: build
	@echo "Important: Notifier runs independently on Windows via GUI. Starting Shield Server locally only."
	@echo "Starting Shield Server..."
	$(MAKE) -C shield-server run

# 独立启动 Shield Server
start-server: build
	$(MAKE) -C shield-server run

# 独立启动 Notifier 的提醒
start-notifier:
	@echo "Notifier runs independently on Windows via GUI. Please start notifier.exe manually on Windows."
