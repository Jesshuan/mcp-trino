#!/usr/bin/env bash
set -euo pipefail

# MCP Trino Server Installation Script
# Usage: ./install.sh [OPTIONS]
# Options:
#   --version=VERSION    Install specific version (default: latest)
#   --install-dir=DIR    Install directory (default: ~/.local/bin)
#   --no-config          Skip Claude configuration
#   --help               Show this help message

# Configuration - can be overridden via environment variables
REPO="tuannvm/mcp-trino"
BINARY_NAME="mcp-trino"
MCP_SERVER_NAME="mcp-trino"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${VERSION:-}"
SKIP_CONFIG=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --version=*)
      VERSION="${1#*=}"
      shift
      ;;
    --install-dir=*)
      INSTALL_DIR="${1#*=}"
      shift
      ;;
    --no-config)
      SKIP_CONFIG=true
      shift
      ;;
    --help)
      echo "MCP Trino Server Installation Script"
      echo "Usage: $0 [OPTIONS]"
      echo ""
      echo "Options:"
      echo "  --version=VERSION    Install specific version (default: latest)"
      echo "  --install-dir=DIR    Install directory (default: ~/.local/bin)"
      echo "  --no-config          Skip Claude configuration"
      echo "  --help               Show this help message"
      echo ""
      echo "Environment variables:"
      echo "  VERSION              Version to install"
      echo "  INSTALL_DIR          Installation directory"
      echo "  GITHUB_TOKEN         GitHub token for API access"
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

# Check for required commands
for cmd in curl tar; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "❌ Error: $cmd is not installed. Please install $cmd to proceed."
    exit 1
  fi
done

# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case $ARCH in
    x86_64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
esac

case $OS in
    darwin) OS="darwin" ;;
    linux) OS="linux" ;;
    *) echo "❌ Unsupported OS: $OS"; exit 1 ;;
esac

echo "🚀 Installing ${BINARY_NAME} for ${OS}-${ARCH}..."

# Get version if not specified
if [[ -z "$VERSION" ]]; then
    echo "📡 Fetching latest release info..."
    if [ -n "${GITHUB_TOKEN:-}" ]; then
        VERSION=$(curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    else
        VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    fi
    
    if [ -z "$VERSION" ]; then
        echo "❌ Error: Unable to get latest version from GitHub API"
        exit 1
    fi
    
    echo "✅ Found latest release: $VERSION"
else
    echo "📌 Using specified version: $VERSION"
fi

# Remove 'v' prefix from version for archive name
VERSION_NO_V="${VERSION#v}"

# Create temporary directory for download
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

# Construct download URL based on GoReleaser naming convention
ARCHIVE_NAME="${BINARY_NAME}_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$ARCHIVE_NAME"

# Download and extract archive
echo "📥 Downloading from: $DOWNLOAD_URL"
if ! curl -fsSL "$DOWNLOAD_URL" | tar -xz -C "$TEMP_DIR" --strip-components=0 "$BINARY_NAME"; then
    echo "❌ Error: Failed to download and extract binary"
    exit 1
fi

# Verify download was successful
if [[ ! -f "${TEMP_DIR}/${BINARY_NAME}" ]] || [[ ! -s "${TEMP_DIR}/${BINARY_NAME}" ]]; then
    echo "❌ Downloaded file is empty or missing"
    exit 1
fi

# Make executable
chmod +x "${TEMP_DIR}/${BINARY_NAME}"

# Create ~/.local/bin directory if it doesn't exist
mkdir -p "$INSTALL_DIR"

# Install binary to ~/.local/bin
echo "📦 Installing to $INSTALL_DIR..."
install -m 755 "${TEMP_DIR}/${BINARY_NAME}" "$INSTALL_DIR/"
echo "✅ Installed to $INSTALL_DIR/${BINARY_NAME}"

# Verify installation
if command -v "${BINARY_NAME}" >/dev/null 2>&1; then
    echo "✅ Installation verified"
else
    echo "⚠️  Binary installed but not found in PATH"
    echo "💡 Add $INSTALL_DIR to your PATH:"
    echo "   export PATH=\"$INSTALL_DIR:\$PATH\""
fi

# Skip configuration if requested
if [[ "$SKIP_CONFIG" == "true" ]]; then
    echo "⏭️  Skipping Claude configuration (--no-config specified)"
    echo ""
    echo "📖 Usage: ${BINARY_NAME} --help"
    echo "📚 Documentation: https://github.com/${REPO}"
    echo "🎉 Installation complete!"
    exit 0
fi

# Claude configuration logic
echo ""
echo "🔍 Detecting Claude installations..."

# Detect installations
CLAUDE_CODE_FOUND=$(command -v claude >/dev/null 2>&1 && echo "true" || echo "false")
CLAUDE_DESKTOP_CONFIG=""
case $OS in
    darwin) CLAUDE_DESKTOP_CONFIG="$HOME/Library/Application Support/Claude/claude_desktop_config.json" ;;
    linux) CLAUDE_DESKTOP_CONFIG="$HOME/.config/Claude/claude_desktop_config.json" ;;
esac
CLAUDE_DESKTOP_FOUND=$([[ -n "$CLAUDE_DESKTOP_CONFIG" ]] && [[ -f "$CLAUDE_DESKTOP_CONFIG" ]] && echo "true" || echo "false")

# Configuration functions
configure_claude_code() {
    echo "🤖 Configuring Claude Code..."
    
    # Check if MCP server already exists
    if claude mcp list 2>/dev/null | grep -q "${MCP_SERVER_NAME}"; then
        echo "⚠️  MCP server '${MCP_SERVER_NAME}' already exists in Claude Code"
        read -p "Do you want to update it? (y/N): " update_choice
        if [[ "$update_choice" =~ ^[Yy]$ ]]; then
            claude mcp remove "${MCP_SERVER_NAME}" 2>/dev/null || true
            if claude mcp add "${MCP_SERVER_NAME}" "$INSTALL_DIR/${BINARY_NAME}"; then
                echo "✅ Claude Code configuration updated successfully!"
            else
                echo "❌ Failed to update. Manual command: claude mcp add ${MCP_SERVER_NAME} \"$INSTALL_DIR/${BINARY_NAME}\""
            fi
        else
            echo "⏭️  Skipping Claude Code configuration"
        fi
    else
        if claude mcp add "${MCP_SERVER_NAME}" "$INSTALL_DIR/${BINARY_NAME}"; then
            echo "✅ Claude Code configured successfully!"
        else
            echo "❌ Failed. Manual command: claude mcp add ${MCP_SERVER_NAME} \"$INSTALL_DIR/${BINARY_NAME}\""
        fi
    fi
}

configure_claude_desktop() {
    echo "🖥️  Configuring Claude Desktop..."
    mkdir -p "$(dirname "$CLAUDE_DESKTOP_CONFIG")"
    
    # Check if MCP server already exists in config
    local server_exists=false
    if [[ -f "$CLAUDE_DESKTOP_CONFIG" ]] && command -v jq >/dev/null 2>&1; then
        if jq -e --arg name "$MCP_SERVER_NAME" '.mcpServers[$name]' "$CLAUDE_DESKTOP_CONFIG" >/dev/null 2>&1; then
            server_exists=true
        fi
    elif [[ -f "$CLAUDE_DESKTOP_CONFIG" ]] && grep -q "\"${MCP_SERVER_NAME}\"" "$CLAUDE_DESKTOP_CONFIG" 2>/dev/null; then
        server_exists=true
    fi
    
    if [[ "$server_exists" == "true" ]]; then
        echo "⚠️  MCP server '${MCP_SERVER_NAME}' already exists in Claude Desktop config"
        read -p "Do you want to update it? (y/N): " update_choice
        if [[ "$update_choice" =~ ^[Yy]$ ]]; then
            echo "🔄 Updating existing configuration..."
        else
            echo "⏭️  Skipping Claude Desktop configuration"
            return
        fi
    fi
    
    # Backup existing config
    [[ -f "$CLAUDE_DESKTOP_CONFIG" ]] && cp "$CLAUDE_DESKTOP_CONFIG" "${CLAUDE_DESKTOP_CONFIG}.backup"
    
    if command -v jq >/dev/null 2>&1; then
        if [[ -f "$CLAUDE_DESKTOP_CONFIG" ]]; then
            jq --arg name "$MCP_SERVER_NAME" --arg cmd "$INSTALL_DIR/${BINARY_NAME}" \
                '.mcpServers += {($name): {"command": $cmd}}' "$CLAUDE_DESKTOP_CONFIG" > "${CLAUDE_DESKTOP_CONFIG}.tmp" && mv "${CLAUDE_DESKTOP_CONFIG}.tmp" "$CLAUDE_DESKTOP_CONFIG"
        else
            jq -n --arg name "$MCP_SERVER_NAME" --arg cmd "$INSTALL_DIR/${BINARY_NAME}" \
                '{mcpServers: {($name): {"command": $cmd}}}' > "$CLAUDE_DESKTOP_CONFIG"
        fi
    else
        echo "⚠️  'jq' is not available. Please update the configuration manually."
        show_manual_config
        return
    fi
    
    if [[ "$server_exists" == "true" ]]; then
        echo "✅ Claude Desktop configuration updated! Restart Claude Desktop to apply changes."
    else
        echo "✅ Claude Desktop configured! Restart Claude Desktop to apply changes."
    fi
}

show_manual_config() {
    echo "📋 Manual Configuration:"
    echo "🤖 Claude Code: claude mcp add ${MCP_SERVER_NAME} \"$INSTALL_DIR/${BINARY_NAME}\""
    echo "🖥️  Claude Desktop: Add to config file ($CLAUDE_DESKTOP_CONFIG):"
    cat << EOF
   {"mcpServers": {"${MCP_SERVER_NAME}": {"command": "${INSTALL_DIR}/${BINARY_NAME}"}}}
EOF
}

# Interactive menu
if [[ "$CLAUDE_CODE_FOUND" == "true" ]] || [[ "$CLAUDE_DESKTOP_FOUND" == "true" ]]; then
    echo "🎯 Found Claude installations:"
    [[ "$CLAUDE_CODE_FOUND" == "true" ]] && echo "   ✅ Claude Code"
    [[ "$CLAUDE_DESKTOP_FOUND" == "true" ]] && echo "   ✅ Claude Desktop"
    echo "🛠️  Choose configuration option:"
    if [[ "$CLAUDE_CODE_FOUND" == "true" ]] && [[ "$CLAUDE_DESKTOP_FOUND" == "true" ]]; then
        echo "   1) Configure both Claude Code and Claude Desktop automatically"
        echo "   2) Configure Claude Code only"
        echo "   3) Configure Claude Desktop only"
        echo "   4) Show manual configuration"
        echo "   5) Skip configuration"
        read -p "Enter choice: " choice
        
        case $choice in
            1) configure_claude_code; configure_claude_desktop ;;
            2) configure_claude_code ;;
            3) configure_claude_desktop ;;
            4) show_manual_config ;;
            5) echo "⏭️  Skipping configuration" ;;
            *) echo "❌ Invalid choice"; show_manual_config ;;
        esac
    else
        [[ "$CLAUDE_CODE_FOUND" == "true" ]] && echo "   1) Configure Claude Code automatically"
        [[ "$CLAUDE_DESKTOP_FOUND" == "true" ]] && echo "   2) Configure Claude Desktop automatically"
        echo "   3) Show manual configuration"
        echo "   4) Skip configuration"
        read -p "Enter choice: " choice
        
        case $choice in
            1) [[ "$CLAUDE_CODE_FOUND" == "true" ]] && configure_claude_code || show_manual_config ;;
            2) [[ "$CLAUDE_DESKTOP_FOUND" == "true" ]] && configure_claude_desktop || show_manual_config ;;
            3) show_manual_config ;;
            4) echo "⏭️  Skipping configuration" ;;
            *) echo "❌ Invalid choice"; show_manual_config ;;
        esac
    fi
else
    echo "❌ No Claude installations detected"
    show_manual_config
fi

echo ""
echo "📖 Usage: ${BINARY_NAME} --help"
echo "📚 Documentation: https://github.com/${REPO}"
echo ""
echo "⚠️  Note: Make sure $INSTALL_DIR is in your PATH for direct command usage"
echo ""
echo "🎉 Installation complete!"
