#!/bin/bash
set -e

echo "========================================================================================="
echo "Running unit tests..."
echo "========================================================================================="
go test ./... -v

echo "========================================================================================="
echo "Building talstomize..."
echo "========================================================================================="
go build -o talstomize ./cmd/talstomize
trap 'rm -f talstomize examples/simple/talos-secrets.yaml; rm -rf examples/simple/_out' EXIT

echo "========================================================================================="
echo "Rendering the example project..."
echo "========================================================================================="
talosctl gen secrets -o examples/simple/talos-secrets.yaml
./talstomize build ./examples/simple

# Talos machine configs include commented-out example values for
# undocumented fields (e.g. "# disk: /dev/sda"), so patched values must be
# matched as actual (uncommented) lines, not plain substrings.
assert_set() {
  if ! grep -F -- "$2" "$1" | grep -vqE '^[[:space:]]*#'; then
    echo "Error: expected to find '${2}' (set, not commented) in $1" >&2
    exit 1
  fi
}

assert_unset() {
  if grep -F -- "$2" "$1" | grep -vqE '^[[:space:]]*#'; then
    echo "Error: did not expect to find '${2}' (set) in $1" >&2
    exit 1
  fi
}

echo "====> Validating nodea (controlplane) got its hostname, cluster-wide, per-node, and role patches, plus its own installImage override..."
assert_set examples/simple/_out/nodea.yaml "hostname: nodea"
assert_set examples/simple/_out/nodea.yaml "net.core.somaxconn:"
assert_set examples/simple/_out/nodea.yaml "disk: /dev/sda"
assert_set examples/simple/_out/nodea.yaml "allowSchedulingOnControlPlanes: true"
assert_set examples/simple/_out/nodea.yaml "image: factory.talos.dev/metal-installer/abc123:v1.9.5"
assert_unset examples/simple/_out/nodea.yaml "image: ghcr.io/siderolabs/installer:v1.9.5"

echo "====> Validating nodeb (worker) got its hostname, cluster-wide, and inline patch, but not nodea's, and fell back to the cluster-wide installImage..."
assert_set examples/simple/_out/nodeb.yaml "hostname: nodeb"
assert_set examples/simple/_out/nodeb.yaml "net.core.somaxconn:"
assert_set examples/simple/_out/nodeb.yaml "destination: /var/lib/longhorn"
assert_unset examples/simple/_out/nodeb.yaml "disk: /dev/sda"
assert_set examples/simple/_out/nodeb.yaml "image: ghcr.io/siderolabs/installer:v1.9.5"

echo "====> Validating a talosctl client config was generated too..."
assert_set examples/simple/_out/talosconfig "context: home-cluster"
assert_set examples/simple/_out/talosconfig "10.5.0.11"

echo "========================================================================================="
echo "Tests completed successfully!"
echo "========================================================================================="
