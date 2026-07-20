#!/usr/bin/env bash

set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root"

container="vault-go-config-integration-${PPID}-$$"
root_token="vault-go-config-dev-only-root-token"
vault_image="${VAULT_IMAGE:-hashicorp/vault:2.0.3}"

cleanup() {
  docker rm --force "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker run --detach \
  --name "$container" \
  --publish 127.0.0.1::8200 \
  --env "VAULT_DEV_ROOT_TOKEN_ID=$root_token" \
  --env VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200 \
  --env VAULT_ADDR=http://127.0.0.1:8200 \
  "$vault_image" server -dev >/dev/null

ready=false
for _attempt in {1..40}; do
  if docker exec "$container" vault status >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 0.25
done
if [[ "$ready" != true ]]; then
  echo "Disposable Vault did not become ready in time." >&2
  exit 1
fi

vault_exec=(docker exec --interactive \
  --env VAULT_ADDR=http://127.0.0.1:8200 \
  --env "VAULT_TOKEN=$root_token" \
  "$container" vault)

"${vault_exec[@]}" kv put secret/integration/config \
  service_name=integration-service workers=3 >/dev/null

printf '%s\n' 'path "secret/data/integration/*" {' \
  '  capabilities = ["read"]' \
  '}' | "${vault_exec[@]}" policy write vault-go-config-read - >/dev/null

read_token="$("${vault_exec[@]}" token create \
  -policy=vault-go-config-read -field=token)"
denied_token="$("${vault_exec[@]}" token create \
  -policy=default -field=token)"

"${vault_exec[@]}" auth enable approle >/dev/null
"${vault_exec[@]}" write auth/approle/role/vault-go-config-integration \
  token_policies=vault-go-config-read >/dev/null
role_id="$("${vault_exec[@]}" read \
  -field=role_id auth/approle/role/vault-go-config-integration/role-id)"
secret_id="$("${vault_exec[@]}" write \
  -field=secret_id -force auth/approle/role/vault-go-config-integration/secret-id)"

username="vault-go-config-integration-user"
password="vault-go-config-integration-password"
"${vault_exec[@]}" auth enable userpass >/dev/null
"${vault_exec[@]}" write "auth/userpass/users/$username" \
  "password=$password" policies=vault-go-config-read >/dev/null

port_mapping="$(docker port "$container" 8200/tcp)"
host_port="${port_mapping##*:}"
export VLTCONFIG_INTEGRATION_VAULT_ADDRESS="http://127.0.0.1:$host_port"
export VLTCONFIG_INTEGRATION_VAULT_READ_TOKEN="$read_token"
export VLTCONFIG_INTEGRATION_VAULT_DENIED_TOKEN="$denied_token"
export VLTCONFIG_INTEGRATION_VAULT_ROLE_ID="$role_id"
export VLTCONFIG_INTEGRATION_VAULT_SECRET_ID="$secret_id"
export VLTCONFIG_INTEGRATION_VAULT_USERNAME="$username"
export VLTCONFIG_INTEGRATION_VAULT_PASSWORD="$password"

go test -tags=integration -count=1 -run '^TestDisposableVault' ./...
