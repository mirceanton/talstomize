# Example: simple

A two-node cluster with one control plane node and one worker. With a cluster-wide patch, a per-role patch, a per-node patch, and an inline patch.

Try it out:

```shell
talosctl gen secrets -o talos-secrets.yaml
go run ../../cmd/talstomize build .
```
