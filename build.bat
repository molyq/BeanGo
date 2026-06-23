@echo off
cd /d "%~dp0web"

echo [1/2] Building frontend...
call npm run build:renderer
if %ERRORLEVEL% neq 0 (
    echo ERROR: Frontend build failed
    exit /b 1
)

echo [2/2] Building Go server...
go build -ldflags="-s -w" -o ..\BeanGo.exe .
if %ERRORLEVEL% neq 0 (
    echo ERROR: Go build failed
    exit /b 1
)

echo.
echo Build complete: %~dp0BeanGo.exe
pause
