# Example: simple

A two-node cluster with one control plane node and one worker. With a per-node patch, a per-role patch, and an inline patch.

Try it out:

```shell
talosctl gen secrets -o talos-secrets.yaml
go run ../../cmd/talstomize build .
```
