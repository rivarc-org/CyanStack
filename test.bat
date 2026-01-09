@echo off
go build -o cyan-cli.exe ./cmd/cyan-cli/...
if %ERRORLEVEL% NEQ 0 (
    echo Build failed
    pause
    exit /b 1
)
cyan-cli.exe test-all
pause
