#!/usr/bin/env sh
# Verifies that every dependency license is in the allowlist (spec §14.5).
# Fails the build if anything GPL/AGPL/unknown creeps in.
set -eu
GOBIN="${GOBIN:-$(go env GOPATH)/bin}"
if [ ! -x "$GOBIN/go-licenses" ]; then
    echo "installing go-licenses..."
    go install github.com/google/go-licenses@latest
fi
"$GOBIN/go-licenses" check ./... \
    --allowed_licenses=MIT,BSD-2-Clause,BSD-3-Clause,Apache-2.0,ISC,MPL-2.0
echo "license check passed"
