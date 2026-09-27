#!/bin/bash
# setup-env.sh - Generate and set environment variables for Local Model Router
# Usage: source setup-env.sh [--admin-password <password>]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Local Model Router - Environment Setup${NC}"
echo "========================================"
echo

# Generate AUTH_ENCRYPTION_KEY (32 bytes = 64 hex characters)
if command -v openssl &> /dev/null; then
    AUTH_ENCRYPTION_KEY=$(openssl rand -hex 32)
elif command -v xxd &> /dev/null && [ -r /dev/urandom ]; then
    AUTH_ENCRYPTION_KEY=$(head -c 32 /dev/urandom | xxd -p -c 64)
else
    # Fallback using $RANDOM (less secure, but works)
    echo -e "${YELLOW}Warning: openssl not found, using fallback method${NC}"
    AUTH_ENCRYPTION_KEY=""
    for i in {1..64}; do
        AUTH_ENCRYPTION_KEY+=$(printf '%x' $((RANDOM % 16)))
    done
fi

# Generate or use provided ADMIN_PASSWORD
if [ "$1" == "--admin-password" ] && [ -n "$2" ]; then
    ADMIN_PASSWORD="$2"
    echo -e "${YELLOW}Using provided admin password${NC}"
else
    # Generate a random password (16 characters, alphanumeric + special)
    if command -v openssl &> /dev/null; then
        ADMIN_PASSWORD=$(openssl rand -base64 16 | tr -dc 'a-zA-Z0-9!@#$%' | head -c 16)
    else
        ADMIN_PASSWORD=$(head -c 100 /dev/urandom | tr -dc 'a-zA-Z0-9' | head -c 16)
    fi
fi

# Export variables
export AUTH_ENCRYPTION_KEY
export ADMIN_PASSWORD

# Display the values
echo -e "${GREEN}Generated environment variables:${NC}"
echo
echo -e "AUTH_ENCRYPTION_KEY=${YELLOW}${AUTH_ENCRYPTION_KEY}${NC}"
echo -e "ADMIN_PASSWORD=${YELLOW}${ADMIN_PASSWORD}${NC}"
echo

# Save to .env file option
read -p "Save to .env file? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    cat > .env << EOF
# Local Model Router Environment Variables
# Generated on $(date)
# WARNING: Keep this file secure and never commit to version control!

AUTH_ENCRYPTION_KEY=${AUTH_ENCRYPTION_KEY}
ADMIN_PASSWORD=${ADMIN_PASSWORD}

# Optional: Ollama endpoint (uncomment to override default)
# OLLAMA_ENDPOINT=http://localhost:11434
EOF
    chmod 600 .env
    echo -e "${GREEN}Saved to .env file (permissions set to 600)${NC}"
fi

echo
echo -e "${GREEN}Environment variables have been exported to current shell.${NC}"
echo -e "${YELLOW}Note: Run 'source setup-env.sh' to export variables to your current session.${NC}"
echo
echo "To start the router:"
echo "  ./bin/local-model-router -config config.example.yaml"
echo
echo "Or with Docker Compose:"
echo "  docker-compose up -d"
