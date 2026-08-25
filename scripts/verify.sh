#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo '==> Shell syntax'
bash -n scripts/*.sh
node --check frontend/public/sw.js

echo '==> Go formatting'
unformatted="$(gofmt -l gateway orchestrator)"
if [[ -n "$unformatted" ]]; then
  printf 'Go files need gofmt:\n%s\n' "$unformatted" >&2
  exit 1
fi

echo '==> Gateway'
(cd gateway && go test ./... && go vet ./...)

echo '==> Orchestrator'
(cd orchestrator && go test ./... && go vet ./...)

echo '==> Frontend'
(cd frontend && npm run build)

echo 'All checks passed.'
