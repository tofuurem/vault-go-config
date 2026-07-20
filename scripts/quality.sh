#!/bin/sh

set -eu

./scripts/fast-check.sh

packages="$(go list ./...)"
if [ -n "$packages" ]; then
	go test -race -coverprofile=coverage.out ./...
	go build ./...
	go tool staticcheck ./...
	go tool govulncheck ./...
fi

(
	unset VAULT_ADDRESS VAULT_TOKEN VAULT_USERNAME VAULT_PASSWORD
	unset VAULT_ROLE_ID VAULT_SECRET_ID VAULT_APP_NAME VAULT_MOUNT_POINT
	unset VAULT_CACHE_DISABLED VAULT_CACHE_TTL VAULT_CACHE_MAX_ENTRIES
	unset PYDANTIC_JSON_PATH
	unset SERVICE_NAME PORT DEBUG
	unset HTTP HTTP__ADDRESS HTTP__READ_TIMEOUT HTTP__WRITE_TIMEOUT LOG_LEVEL
	unset DB DB__HOST DB__PORT DB__POOL DB__POOL__MAX_OPEN DB__POOL__MAX_IDLE

	go run ./examples/basic >/dev/null
	go run ./examples/defaults >/dev/null
	go run ./examples/nested >/dev/null
)
