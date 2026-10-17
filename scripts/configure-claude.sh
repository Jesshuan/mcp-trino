#!/usr/bin/env bash
set -euo pipefail

# MCP Trino - Optional AI Client Configuration (Claude Code / Claude Desktop)
#
# This script is intentionally separate from install.sh: the MCP server
# itself is client-agnostic and can be used with any MCP client
# (Claude Code, Claude Desktop, Cursor, custom clients, etc.).
# Registering it in a specific client is an opt-in, client-specific step.
#
# Usage: scripts/configure-claude.sh [BINARY_PATH]
#   BINARY_PATH defaults to ./bin/mcp-trino, falling back to
#   ~/.local/bin/mcp-trino if ./bin doesn't exist.
#
# Environment variables:
#   MCP_SERVER_NAME    Server name registered in the client (default: mcp-trino)

MCP_SERVER_NAME="${MCP_SERVER_NAME:-mcp-trino}"
REPO="tuannvm/mcp-trino"

# Resolve binary path
BINARY_PATH="${1:-}"
if [[ -z "$BINARY_PATH" ]]; then
    if [[ -x "bin/mcp-trino" ]]; then
        BINARY_PATH="$(pwd)/bin/mcp-trino"
    elif [[ -x "$HOME/.local/bin/mcp-trino" ]]; then
        BINARY_PATH="$HOME/.local/bin/mcp-trino"
    else
        echo "❌ Error: no mcp-trino binary found."
        echo "💡 Build one first: ./install.sh (creates ./bin/mcp-trino)"
        exit 1
    fi
fi

if [[ ! -x "$BINARY_PATH" ]]; then
    echo "❌ Error: $BINARY_PATH is not an executable file."
    exit 1
fi

BINARY_PATH="$(cd "$(dirname "$BINARY_PATH")" && pwd)/$(basename "$BINARY_PATH")"
echo "🔗 Using binary: $BINARY_PATH"

# Detect OS and Claude installations
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
CLAUDE_DESKTOP_CONFIG=""
case $OS in
    darwin) CLAUDE_DESKTOP_CONFIG="$HOME/Library/Application Support/Claude/claude_desktop_config.json" ;;
    linux) CLAUDE_DESKTOP_CONFIG="$HOME/.config/Claude/claude_desktop_config.json" ;;
esac

CLAUDE_CODE_FOUND=$(command -v claude >/dev/null 2>&1 && echo "true" || echo "false")
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
            if claude mcp add "${MCP_SERVER_NAME}" "$BINARY_PATH"; then
                echo "✅ Claude Code configuration updated successfully!"
            else
                echo "❌ Failed to update. Manual command: claude mcp add ${MCP_SERVER_NAME} \"$BINARY_PATH\""
            fi
        else
            echo "⏭️  Skipping Claude Code configuration"
        fi
    else
        if claude mcp add "${MCP_SERVER_NAME}" "$BINARY_PATH"; then
            echo "✅ Claude Code configured successfully!"
        else
            echo "❌ Failed. Manual command: claude mcp add ${MCP_SERVER_NAME} \"$BINARY_PATH\""
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
            jq --arg name "$MCP_SERVER_NAME" --arg cmd "$BINARY_PATH" \
                '.mcpServers += {($name): {"command": $cmd}}' "$CLAUDE_DESKTOP_CONFIG" > "${CLAUDE_DESKTOP_CONFIG}.tmp" && mv "${CLAUDE_DESKTOP_CONFIG}.tmp" "$CLAUDE_DESKTOP_CONFIG"
        else
            jq -n --arg name "$MCP_SERVER_NAME" --arg cmd "$BINARY_PATH" \
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
    echo "📋 Manual Configuration (any MCP client):"
    echo "🤖 Claude Code: claude mcp add ${MCP_SERVER_NAME} \"$BINARY_PATH\""
    echo "🖥️  Claude Desktop: Add to config file ($CLAUDE_DESKTOP_CONFIG):"
    cat << EOF
   {"mcpServers": {"${MCP_SERVER_NAME}": {"command": "${BINARY_PATH}"}}}
EOF
    echo ""
    echo "📎 Other MCP clients: point them at the binary (stdio) or at"
    echo "   http://localhost:8080/mcp when started with MCP_TRANSPORT=http."
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
echo "📚 Documentation: https://github.com/${REPO}"
