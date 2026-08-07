# talstomize

Like kustomize, but for Talos!

`talstomize` renders per-node [Talos Linux](https://www.talos.dev) machine configuration from a single, declarative file: default cluster settings plus a list of nodes, with patches layered on by role (`controlplane` / `worker`) and then by individual node.  
The same mental model as Kustomize bases and overlays, applied to `talosctl gen config` output instead of Kubernetes manifests.

## Install

> [!NOTE]
> `talstomize apply` and `talstomize diff` shell out to `talosctl`, so it must also be on your `PATH`, whichever install method you use.  
>  
> If your secrets bundle is sops-encrypted, [`sops`](https://github.com/getsops/sops) must be on `PATH` too - talstomize shells out to it rather than embedding it, so plaintext bundles need no extra tooling at all.

### Download precompiled binaries

Grab the archive for your platform from the
[GitHub Releases page](https://github.com/mirceanton/talstomize/releases/latest).

### Install via Homebrew

```shell
brew tap mirceanton/taps
brew install talstomize
```

### Running via Docker

```shell
docker pull ghcr.io/mirceanton/talstomize
```

### Install via `go install`

```shell
go install github.com/mirceanton/talstomize/cmd/talstomize@latest
```

## Usage

1. Generate a secrets bundle once per cluster (talstomize never generates or rotates secrets itself, it only reads them):

   ```shell
   talosctl gen secrets -o talos-secrets.yaml
   ```

2. Write a `talstomize.yaml`:

   ```yaml
   apiVersion: config.talstomize.dev/v1alpha1
   kind: Talstomize

   clusterName: my-cluster
   controlPlaneEndpoint: https://10.5.0.2:6443
   secrets: ./talos-secrets.yaml

   nodes:
     nodea:
       ip: 10.5.0.11
       kind: controlplane
       patches:
         - ./patches/nodea-disk.yaml
     nodeb:
       ip: 10.5.0.12
       kind: worker
       patches:
         - machine:
             kubelet:
               extraMounts:
                 - destination: /var/lib/longhorn
                   type: bind
                   source: /var/lib/longhorn
                   options: ["bind", "rshared", "rw"]

   # Applied to every node, regardless of role.
   patches:
     - machine:
         sysctls:
           net.core.somaxconn: "1024"

   # Applied to every controlplane / worker node, before that node's own patches.
   controlplanePatches:
     - ./patches/controlplane-common.yaml

   workerPatches: []
   ```

3. Render the configs (one file per node, written as `<node>.yaml` to `./_out` next to the `talstomize.yaml` by default, plus a `talosctl` client config named `talosconfig`):

   ```shell
   talstomize build .                  # writes ./_out/nodea.yaml, ./_out/nodeb.yaml, ./_out/talosconfig, ...
   talstomize build . -o ./rendered    # write to ./rendered instead
   ```

4. Or apply them straight to the nodes:

   ```shell
   talstomize apply -f .                           # every node
   talstomize apply -f . --node nodea              # just one
   talstomize apply -f . -- --insecure             # first apply, maintenance mode
   talstomize apply -f . -- --insecure --dry-run   # multiple flags at once
   ```

5. Or check what would change before applying anything:

   ```shell
   talstomize diff -f .                # every node
   talstomize diff -f . --node nodea   # just one
   ```

   ```console
   $ talstomize diff -f .
   ==> nodea (10.5.0.11):
   --- a
   +++ b
   @@ -12,3 +12,3 @@
        install:
   -        image: ghcr.io/siderolabs/installer:v1.9.5
   +        image: ghcr.io/siderolabs/installer:v1.9.6
   ==> nodeb (10.5.0.12): no differences
   ```

   Exits `0` if every node matches, `1` if any node differs (or on error) -
   safe to use in a script or CI check.

>[!IMPORTANT]
> `apply` and `diff` are thin wrappers. `apply` renders each node's config the same way `build` does, then runs `talosctl apply-config --nodes <ip> --file <rendered>` for it. `diff` renders it the same way, fetches the node's currently running config via `talosctl get machineconfig --nodes <ip> -o yaml`, and diffs the two (encoded without comments, so the diff isn't swamped by Talos's per-field documentation).  
> Everything after `--` is passed straight through to the underlying `talosctl` invocation, so any of its flags work (`--insecure`, `--dry-run`, `--mode`, `--talosconfig`, `--endpoints`, ...) without talstomize needing to know about them.

See [`examples/simple`](./examples/simple) for a complete working example.

## Config reference

| Field                          | Description                                                                                   |
| ------------------------------ | ----------------------------------------------------------------------------------------------- |
| `apiVersion` / `kind`          | Must be `config.talstomize.dev/v1alpha1` / `Talstomize`.                                        |
| `clusterName`                  | Passed to `talosctl gen config` equivalent as the cluster name.                                 |
| `controlPlaneEndpoint`         | The cluster's control plane endpoint (e.g. a VIP or load balancer URL).                         |
| `kubernetesVersion`            | Optional; defaults to the version bundled with talstomize's Talos machinery dependency. A leading `v` (e.g. `v1.31.1`) is accepted and stripped. |
| `secrets`                      | Path to a secrets bundle produced by `talosctl gen secrets`. May be sops-encrypted.              |
| `additionalSubjectAltNames`    | Optional; extra SANs added to both the machine and kube-apiserver certificates on every node (`talosctl gen config`'s `--additional-sans`). |
| `dnsDomain`                    | Optional; the cluster's DNS domain (`talosctl gen config`'s `--dns-domain`). Defaults to `cluster.local`. |
| `installer.image`              | Optional; the cluster-wide default installer image (`talosctl gen config`'s `--install-image`). Overridable per node. Unlike bare `talosctl`, no smart default is computed - leave `installer` unset and `machine.install.image` is left for patches to set. Mutually exclusive with `installer.schematic`. |
| `installer.schematic`          | Optional; a [Image Factory](https://factory.talos.dev) schematic customization document (`extraKernelArgs`, `systemExtensions`, ...), resolved to a schematic ID and combined with `installer.talosVersion` into the cluster-wide default installer image. Overridable (fully replaced, not merged) per node. Mutually exclusive with `installer.image`. Only the `metal` platform is supported for now. |
| `installer.talosVersion`       | Optional; tags the `installer.schematic`-computed installer image (e.g. `v1.13.8`). Only consulted when `installer.schematic` is set. Defaults to the Talos version bundled with talstomize's machinery dependency. **Not** the same thing as `talosctl gen config`'s `--talos-version`, which is an unrelated backwards-compat config-schema flag, not an installer image tag. |
| `nodes.<name>.ip`              | The node's address, used both as an API server SAN input and as the `talosctl` target.          |
| `nodes.<name>.kind`            | `controlplane` or `worker`.                                                                     |
| `nodes.<name>.installer.image` | Optional; overrides `installer.image` for this node only. Mutually exclusive with `nodes.<name>.installer.schematic`. No `nodes.<name>.installer.talosVersion` - that's cluster-wide only. |
| `nodes.<name>.installer.schematic` | Optional; overrides `installer.schematic` for this node only (a full replacement, not merged). Mutually exclusive with `nodes.<name>.installer.image`. |
| `nodes.<name>.patches`         | Patches applied to this node only, after the cluster-wide and role-wide patches.                |
| `patches`                      | Patches applied to every node, regardless of role - the equivalent of `talosctl gen config`'s unprefixed `--config-patch`. |
| `controlplanePatches`          | Patches applied to every `controlplane` node.                                                   |
| `workerPatches`                | Patches applied to every `worker` node.                                                         |

Patches are applied in order: implicit hostname patch → `patches` (cluster-wide) → role patches (`controlplanePatches`/`workerPatches`) → node patches, each overriding the ones before it.

Each patch entry is either:

- a **string**, treated as either:
  - a **path** to a patch file, relative to the `talstomize.yaml` it's declared in,
  - an **https URL** (e.g. a GitHub raw URL), fetched over the network -
    handy for sharing a common patch across clusters/repos without
    copy-pasting it. Plain `http://` is rejected: patch content becomes
    machine config, and an unauthenticated response can be tampered with
    in transit. Prefer pinning to a commit SHA rather than a branch name
    for a reproducible build (e.g.
    `https://raw.githubusercontent.com/<user>/<repo>/<sha>/patch.yaml`).
- an **inline YAML document**, used as the patch directly.

Both strategic-merge patches (a partial Talos machine config) and JSON6902 patches are supported, exactly as with `talosctl`'s `--config-patch`.

## Environment variable substitution

`talstomize.yaml` and every patch (file-based or inline) are expanded for `${VAR}` references before being parsed, so secrets like registry credentials don't have to be committed in plain text:

```yaml
# patches/registry.yaml
machine:
  registries:
    config:
      registry.example.com:
        auth:
          username: ${REGISTRY_USERNAME}
          password: ${REGISTRY_PASSWORD}
```

```shell
REGISTRY_USERNAME=... REGISTRY_PASSWORD=... talstomize build .

# or, with a secrets manager that populates the environment for you:
op run --env-file=.env -- talstomize build .
```

If a `.env` file sits next to `talstomize.yaml`, it's loaded automatically (no need to source it yourself first) - handy for local dev, though anything containing real secrets should stay out of git (`echo .env >> .gitignore`). It follows the usual dotenv convention: it never overrides a variable that's already set, so an explicit `export` or `op run` still wins over whatever the file has.

If a `${VAR}` is referenced but unset in the environment, talstomize fails the build rather than silently rendering an empty value.

Substitution runs over the whole raw file, comments included - a literal `${...}`-looking string in a comment is expanded too.

## Sops-encrypted secrets bundle

The `secrets:` bundle (`talosctl gen secrets` output) can be committed to git encrypted with [`sops`](https://github.com/getsops/sops). talstomize detects this automatically - a plain YAML file (no top-level `sops:` key) is read as-is; a sops-encrypted one is transparently decrypted first, by shelling out to `sops`, which must be on `PATH` for this to work:

```shell
talosctl gen secrets -o talos-secrets.yaml
sops --encrypt --age <recipient> talos-secrets.yaml > talos-secrets.sops.yaml
mv talos-secrets.sops.yaml talos-secrets.yaml   # commit this
```

```yaml
# talstomize.yaml
secrets: ./talos-secrets.yaml   # now sops-encrypted, safe to commit
```

sops resolves decryption keys the same way it always does (`SOPS_AGE_KEY_FILE`, `SOPS_AGE_KEY`, a GPG keyring, cloud KMS credentials, ...) - talstomize doesn't get involved in key management at all.

## Image Factory schematics

For a node that needs Talos system extensions (drivers, `zfs`, `nvidia`, ...) or extra kernel args, `installer.schematic` posts a customization
document to the [Image Factory](https://factory.talos.dev) and turns the resulting schematic ID into the node's installer image. It lives under `installer` alongside `installer.image`, mirroring how `machine.install` itself nests image config in the underlying Talos schema, rather than as flat top-level fields:

```yaml
# talstomize.yaml
installer:
  talosVersion: v1.13.8
  schematic:
    customization:
      extraKernelArgs:
        - cpufreq.default_governor=performance
      systemExtensions:
        officialExtensions:
          - siderolabs/zfs
          - siderolabs/iscsi-tools
```

This resolves (once per unique customization, memoized - a schematic shared by several nodes only hits the Factory once) to `machine.install.image: factory.talos.dev/metal-installer/<schematic id>:v1.13.8`, applied the same way an explicit `installer.image` would be. `nodes.<name>.installer.schematic` overrides it per node, for hardware that needs its own extensions (e.g. one node with a GPU).

>[!NOTE]
> Unlike everything else talstomize does, resolving a schematic needs network access to `factory.talos.dev` - `build`/`apply` will fail if it's unreachable. Leaving `installer` unset entirely avoids this.
