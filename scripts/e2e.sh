#!/usr/bin/env sh
set -eu

COMPOSE="docker compose -f docker-compose.yml -f docker-compose.e2e.yml"
export APP_PORT="${APP_PORT:-18080}"
export LLM_API_KEY="${LLM_API_KEY:-e2e-key}"
export AUTH_SECRET="${AUTH_SECRET:-e2e-secret}"
cleanup() {
  $COMPOSE down
}
trap cleanup EXIT INT TERM

$COMPOSE up -d --build

attempt=0
until curl -fsS "http://localhost:${APP_PORT}/healthz" >/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 40 ]; then
    $COMPOSE logs app mysql redis mock-llm
    exit 1
  fi
  sleep 3
done

curl -fsS "http://localhost:${APP_PORT}/" | grep -q 'Packaging AI'
E2E_BASE_URL="http://localhost:${APP_PORT}" go test ./e2e -v -count=1
