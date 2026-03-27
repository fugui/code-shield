.PHONY: all build clean frontend backend run

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
	go build -o code-shield-server

# 快捷启动命令
run: build
	./code-shield-server
