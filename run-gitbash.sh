#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
go build -o local-file-explorer.exe .
exec ./local-file-explorer.exe
