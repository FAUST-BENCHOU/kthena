#!/usr/bin/env bash
# Helm post-renderer: must read rendered YAML from stdin and write patched YAML to stdout.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd)"
CONFIG_PATH="${SESSIONSTICKY_ROUTER_CONFIG:?SESSIONSTICKY_ROUTER_CONFIG is not set}"
cd "$REPO_ROOT"
exec go run ./test/e2e/router/sessionsticky/postrender_tool "$CONFIG_PATH"
