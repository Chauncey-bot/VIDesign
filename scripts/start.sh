#!/usr/bin/env sh
set -eu

if [ ! -f .env ]; then echo "Missing .env. Run: cp .env.example .env, then fill in the real LLM_API_KEY and AUTH_SECRET." >&2; exit 1; fi
docker compose up -d --build
echo "Packaging AI started at http://localhost:${APP_PORT:-8080}"
