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

# Image Factory schematic IDs are content hashes of the customization
# document, so they're stable for a given example schematic.
SCHEMATIC_CLUSTER=c9078f9419961640c712a8bf2bb9174933dfcf1da383fd8ea2b7dc21493f8bac # iscsi-tools
SCHEMATIC_NODEA=98c912e25a452279d00bd5f060b0b3f6677ee1546f391928bab5ed1e5bd90bd7   # iscsi-tools + zfs

echo "====> Validating nodea (controlplane) got its hostname, cluster-wide, per-node, and role patches, plus its own installer override..."
assert_set examples/simple/_out/nodea.yaml "hostname: nodea"
assert_set examples/simple/_out/nodea.yaml "net.core.somaxconn:"
assert_set examples/simple/_out/nodea.yaml 'match: disk.dev_path == "/dev/sda"'
assert_unset examples/simple/_out/nodea.yaml "taints:"
assert_set examples/simple/_out/nodea.yaml "image: factory.talos.dev/metal-installer/${SCHEMATIC_NODEA}:v1.14.2"
assert_unset examples/simple/_out/nodea.yaml "image: factory.talos.dev/metal-installer/${SCHEMATIC_CLUSTER}:v1.14.2"

echo "====> Validating nodeb (worker) got its hostname, cluster-wide, and inline patches, but not nodea's, and fell back to the cluster-wide installer..."
assert_set examples/simple/_out/nodeb.yaml "hostname: nodeb"
assert_set examples/simple/_out/nodeb.yaml "net.core.somaxconn:"
assert_set examples/simple/_out/nodeb.yaml "- time.cloudflare.com"
assert_set examples/simple/_out/nodeb.yaml 'match: disk.dev_path == "/dev/nvme0n1"'
assert_unset examples/simple/_out/nodeb.yaml 'match: disk.dev_path == "/dev/sda"'
assert_set examples/simple/_out/nodeb.yaml "image: factory.talos.dev/metal-installer/${SCHEMATIC_CLUSTER}:v1.14.2"

echo "====> Validating a talosctl client config was generated too..."
assert_set examples/simple/_out/talosconfig "context: home-cluster"
assert_set examples/simple/_out/talosconfig "10.5.0.11"

echo "====> Validating node configuration using talosctl..."
talosctl validate -m metal -c examples/simple/_out/nodea.yaml
talosctl validate -m metal -c examples/simple/_out/nodeb.yaml

echo "========================================================================================="
echo "Tests completed successfully!"
echo "========================================================================================="
