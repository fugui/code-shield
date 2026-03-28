@echo off
echo =========================================
echo   Building Code-Shield Notifier (GUI)
echo =========================================

set GOOS=windows
set GOARCH=amd64

echo.
echo Compiling...
go build -ldflags "-H=windowsgui -s -w" -o notifier.exe

if %errorlevel% neq 0 (
    echo.
    echo [ERROR] Build failed!
    pause
    exit /b %errorlevel%
)

echo.
echo [SUCCESS] Build complete!
echo executable generated: notifier.exe
echo You can now double-click notifier.exe to run it without the console window.
echo.
pause
