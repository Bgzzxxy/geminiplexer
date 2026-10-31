@echo off
setlocal
title Geminiplexer Gateway

echo ========================================================
echo        Geminiplexer - High Performance Gateway
echo ========================================================
echo.

if not exist accounts.yaml (
    if exist accounts.example.yaml (
        echo [INFO] accounts.yaml not found. Creating from accounts.example.yaml...
        copy accounts.example.yaml accounts.yaml
        echo [NOTICE] Please open accounts.yaml and paste your Google Gemini cookies!
        notepad accounts.yaml
        pause
    ) else (
        echo [ERROR] Neither accounts.yaml nor accounts.example.yaml found!
        pause
        exit /b 1
    )
)

echo [STARTING] Launching Geminiplexer Gateway on http://127.0.0.1:4981 ...
geminiplexer.exe -config accounts.yaml

if %ERRORLEVEL% NEQ 0 (
    echo.
    echo [ERROR] Server exited with error code %ERRORLEVEL%
    pause
)
