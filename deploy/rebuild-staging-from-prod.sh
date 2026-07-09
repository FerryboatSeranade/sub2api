#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
PROD_COMPOSE="${SCRIPT_DIR}/docker-compose.prod-local.yml"
STAGING_COMPOSE="${SCRIPT_DIR}/docker-compose.staging.yml"
ENV_FILE="${SCRIPT_DIR}/.env"
STAGING_DIR="${SCRIPT_DIR}/staging"
BACKUP_DIR="${STAGING_DIR}/backups"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP_FILE="${BACKUP_DIR}/prod-before-staging-${STAMP}.dump"

PROD_PROJECT="${PROD_PROJECT:-sub2api-dev}"
STAGING_PROJECT="${STAGING_PROJECT:-sub2api-staging}"
PROD_APP_CONTAINER="${PROD_APP_CONTAINER:-sub2api-dev}"
PROD_DB_CONTAINER="${PROD_DB_CONTAINER:-sub2api-dev-postgres}"
STAGING_APP_CONTAINER="${STAGING_APP_CONTAINER:-sub2api-staging}"
STAGING_DB_CONTAINER="${STAGING_DB_CONTAINER:-sub2api-staging-postgres}"
STAGING_PORT="${STAGING_SERVER_PORT:-8081}"
export STAGING_SERVER_PORT="${STAGING_PORT}"

compose() {
  local project="$1"
  local file="$2"
  shift 2
  if [[ -f "${ENV_FILE}" ]]; then
    docker compose --env-file "${ENV_FILE}" -p "${project}" -f "${file}" "$@"
  else
    docker compose -p "${project}" -f "${file}" "$@"
  fi
}

container_project() {
  docker ps --all --filter "name=^/${1}$" --format '{{.Label "com.docker.compose.project"}}' 2>/dev/null | head -n 1
}

container_config_file() {
  docker ps --all --filter "name=^/${1}$" --format '{{.Label "com.docker.compose.project.config_files"}}' 2>/dev/null | head -n 1
}

container_exists() {
  [[ -n "$(docker ps --all --filter "name=^/${1}$" --format '{{.Names}}' 2>/dev/null | head -n 1)" ]]
}

require_container_project() {
  local container="$1"
  local expected="$2"
  local actual
  actual="$(container_project "${container}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "Refusing to continue: ${container} belongs to project '${actual}', expected '${expected}'." >&2
    exit 1
  fi
}

require_file_match() {
  local container="$1"
  local expected="$2"
  local actual
  actual="$(container_config_file "${container}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "Refusing to continue: ${container} was started from '${actual}', expected '${expected}'." >&2
    exit 1
  fi
}

echo "==> Verifying compose boundaries"
require_container_project "${PROD_APP_CONTAINER}" "${PROD_PROJECT}"
require_container_project "${PROD_DB_CONTAINER}" "${PROD_PROJECT}"
require_file_match "${PROD_APP_CONTAINER}" "${PROD_COMPOSE}"
require_file_match "${PROD_DB_CONTAINER}" "${PROD_COMPOSE}"

if container_exists "${STAGING_APP_CONTAINER}"; then
  require_container_project "${STAGING_APP_CONTAINER}" "${STAGING_PROJECT}"
  require_file_match "${STAGING_APP_CONTAINER}" "${STAGING_COMPOSE}"
fi
if container_exists "${STAGING_DB_CONTAINER}"; then
  require_container_project "${STAGING_DB_CONTAINER}" "${STAGING_PROJECT}"
  require_file_match "${STAGING_DB_CONTAINER}" "${STAGING_COMPOSE}"
fi

if [[ "${STAGING_PROJECT}" == "${PROD_PROJECT}" ]]; then
  echo "Refusing to continue: staging project equals production project." >&2
  exit 1
fi
if [[ "${STAGING_DIR}" == "${SCRIPT_DIR}" || "${STAGING_DIR}" == "/" ]]; then
  echo "Refusing to continue: invalid staging data directory '${STAGING_DIR}'." >&2
  exit 1
fi

mkdir -p "${BACKUP_DIR}"

echo "==> Dumping production database from ${PROD_DB_CONTAINER}"
docker exec "${PROD_DB_CONTAINER}" sh -lc 'pg_dump -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" -Fc' > "${BACKUP_FILE}"
chmod 600 "${BACKUP_FILE}"
echo "Backup written: ${BACKUP_FILE}"

echo "==> Stopping and removing staging stack only (${STAGING_PROJECT})"
compose "${STAGING_PROJECT}" "${STAGING_COMPOSE}" down --remove-orphans

echo "==> Removing staging-only data directories"
rm -rf "${STAGING_DIR}/postgres_data" "${STAGING_DIR}/redis_data" "${STAGING_DIR}/data"
mkdir -p "${STAGING_DIR}/postgres_data" "${STAGING_DIR}/redis_data" "${STAGING_DIR}/data" "${BACKUP_DIR}"

echo "==> Starting staging database services"
compose "${STAGING_PROJECT}" "${STAGING_COMPOSE}" up -d --build postgres redis

echo "==> Waiting for staging database"
for _ in $(seq 1 60); do
  if docker exec "${STAGING_DB_CONTAINER}" sh -lc 'pg_isready -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}"' >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
docker exec "${STAGING_DB_CONTAINER}" sh -lc 'pg_isready -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}"'

echo "==> Restoring production dump into staging database"
cat "${BACKUP_FILE}" | docker exec -i "${STAGING_DB_CONTAINER}" sh -lc '
  set -e
  export PGUSER="${POSTGRES_USER:-sub2api}"
  export PGDATABASE="${POSTGRES_DB:-sub2api}"
  psql -v ON_ERROR_STOP=1 -d postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '\''${PGDATABASE}'\'' AND pid <> pg_backend_pid();" >/dev/null
  dropdb --if-exists "${PGDATABASE}"
  createdb "${PGDATABASE}"
  pg_restore --no-owner --role="${PGUSER}" -d "${PGDATABASE}"
'

echo "==> Starting staging app so migrations run on restored data"
compose "${STAGING_PROJECT}" "${STAGING_COMPOSE}" up -d --build sub2api

echo "==> Waiting for staging app health"
for _ in $(seq 1 90); do
  if curl -fsS "http://127.0.0.1:${STAGING_PORT}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
curl -fsS "http://127.0.0.1:${STAGING_PORT}/health" >/dev/null

echo "==> Done. Staging is on host port ${STAGING_PORT}; production ${PROD_PROJECT} was not stopped."
