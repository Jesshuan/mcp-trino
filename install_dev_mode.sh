#!/usr/bin/env bash
set -euo pipefail

# MCP Trino - Development Install Script
#
# Builds the CLI/MCP server from source for local experimentation.
# No release download, no AI-client configuration: the MCP server is
# client-agnostic.
#
# Optional Claude Code / Claude Desktop setup lives in a separate script:
#   scripts/configure-claude.sh
#
# Usage: ./install.sh [OPTIONS]
# Options:
#   --install-dir=DIR    Also copy the binary to DIR (default: none, keep in ./bin)
#   --no-build           Skip the build (binary must already exist in ./bin)
#   --help               Show this help message

BINARY_NAME="mcp-trino"
BUILD_DIR="bin"
INSTALL_DIR=""
SKIP_BUILD=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --install-dir=*)
      INSTALL_DIR="${1#*=}"
      shift
      ;;
    --no-build)
      SKIP_BUILD=true
      shift
      ;;
    --help)
      cat << EOF
MCP Trino - Development Install Script

Builds the binary from source (./cmd) into ./${BUILD_DIR}/ for local
development and experimentation with the CLI.

Usage: $0 [OPTIONS]

Options:
  --install-dir=DIR    Also copy the binary to DIR (default: none)
  --no-build           Skip the build (binary must already exist)
  --help               Show this help message

Environment variables:
  VERSION              Version string to inject (default: git describe or "dev")

Requirements:
  - Go 1.24+ (https://go.dev/dl/)

After install, experiment with:
  ./${BUILD_DIR}/${BINARY_NAME} --help
  make run-dev                # go run ./cmd
  ./${BUILD_DIR}/${BINARY_NAME} --interactive   # psql-like REPL

Optional client integration (Claude Code / Claude Desktop):
  scripts/configure-claude.sh
EOF
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

# Check for Go toolchain
if ! command -v go >/dev/null 2>&1; then
    echo "❌ Error: Go is not installed. Install it from https://go.dev/dl/"
    exit 1
fi

echo "✅ Using Go $(go version | awk '{print $3}')"

# Determine version string
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"

# Build from source
if [[ "$SKIP_BUILD" == "true" ]]; then
    if [[ ! -f "${BUILD_DIR}/${BINARY_NAME}" ]]; then
        echo "❌ Error: ${BUILD_DIR}/${BINARY_NAME} not found. Run without --no-build."
        exit 1
    fi
    echo "⏭️  Skipping build (--no-build specified), using existing ./${BUILD_DIR}/${BINARY_NAME}"
else
    echo "🔨 Building ${BINARY_NAME} (version: ${VERSION})..."
    mkdir -p "$BUILD_DIR"
    go build -ldflags "-X main.Version=${VERSION}" -o "${BUILD_DIR}/${BINARY_NAME}" ./cmd
    echo "✅ Built ./${BUILD_DIR}/${BINARY_NAME}"
fi

# Optional: copy to an install directory
if [[ -n "$INSTALL_DIR" ]]; then
    mkdir -p "$INSTALL_DIR"
    install -m 755 "${BUILD_DIR}/${BINARY_NAME}" "$INSTALL_DIR/"
    echo "✅ Installed to $INSTALL_DIR/${BINARY_NAME}"
    if ! command -v "$BINARY_NAME" >/dev/null 2>&1; then
        echo "💡 If not in PATH, add it: export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi
fi

# Dev-mode usage hints
echo ""
echo "🧪 Experiment with the CLI:"
echo "   ./${BUILD_DIR}/${BINARY_NAME} --help"
echo "   ./${BUILD_DIR}/${BINARY_NAME} --version"
echo "   ./${BUILD_DIR}/${BINARY_NAME} --interactive          # REPL mode"
echo "   ./${BUILD_DIR}/${BINARY_NAME} query 'SELECT 1'        # needs TRINO_HOST/TRINO_USER"
echo "   make run-dev                                          # run from source without building"
echo ""
echo "🔌 Run as an MCP server (client-agnostic):"
echo "   ./${BUILD_DIR}/${BINARY_NAME}                # stdio transport (default)"
echo "   MCP_TRANSPORT=http ./${BUILD_DIR}/${BINARY_NAME}   # HTTP on :8080/mcp"
echo ""
echo "🤖 Optional: configure an AI client (Claude Code / Claude Desktop):"
echo "   scripts/configure-claude.sh"
echo ""
echo "🎉 Development install complete!"
