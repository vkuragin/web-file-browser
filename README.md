# File Explorer

A read-only local file browser written in Go. It starts with a list of available drives, includes hidden and system entries, supports folder navigation, and shows selected item properties. The server allows navigation within discovered drives and listens on localhost only.

## Run on Windows

Install Go, then run `run-windows.ps1` from PowerShell:

```powershell
.\run-windows.ps1
```

## Run in Git Bash

Install Go for Windows and run:

```bash
bash ./run-gitbash.sh
```

Both launchers open the app server at [http://localhost:8765](http://localhost:8765). Press Ctrl+C in that terminal to stop it. Set `FILE_EXPLORER_PORT` to use a different port.

## Tests

```powershell
go test ./...
```
