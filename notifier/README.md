# Code-Shield Notifier

Code-Shield Notifier 是一个在 Windows 后台运行的系统托盘应用程序 (Tray App)。它主要负责监听本地端口，接收包含 Markdown 内容的通知请求，并将它们转换为格式化的 HTML 以及 PDF 附件，最后通过本地的 Outlook 客户端发送邮件或保存到草稿箱。

## 主要功能

- **本地 HTTP 服务**：在 `0.0.0.0:8081` 监听通知请求 (如 `/api/notify/email`)。
- **Outlook 集成**：利用 Windows COM 接口与本地 Outlook 交互，支持直接发送邮件或保存到草稿箱。
- **邮件模板管理**：支持在界面中查看、自定义和保存邮件模板。
- **自动/手动发送**：可通过界面勾选“自动发送”。如果未勾选，所有通过 API 发送过来的请求将转化为 Outlook 中的草稿，并可以在 GUI 中一键“批量发送等待中的邮件”。
- **系统托盘运行**：关闭主窗口后，程序会自动隐藏在系统托盘中后台运行，不会干扰正常的桌面操作。

## 编译构建

本项目使用 Go 语言开发，由于调用了 Windows 原生 GUI 接口（使用 `windigo` 库），需要在能够编译 Windows 程序的平台上进行编译。

可以使用 `Makefile` 或者 `build.bat` 编译：

```bash
# 使用 Makefile
make build

# 或者直接执行 go build（推荐添加 -H=windowsgui 隐藏黑色命令行窗口）
GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui -s -w" -o notifier.exe
```

## 运行与使用

1. 双击生成的 `notifier.exe` 即可运行。
2. 程序启动后会在任务栏右下角系统托盘显示一个图标，双击图标可唤出主控制界面。
3. 如果 8081 端口被占用，程序启动时会弹出报错对话框提示，请确保没有旧的进程残留。
