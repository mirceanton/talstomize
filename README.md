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
   talosVersion: v1.14.2       # every node runs this; selects the generated config style
   kubernetesVersion: v1.37.1

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
         - apiVersion: v1alpha1
           kind: UnattendedInstallConfig
           provisioning:
             diskSelector:
               match: disk.dev_path == "/dev/nvme0n1"

   # Applied to every node, regardless of role.
   patches:
     - apiVersion: v1alpha1
       kind: SysctlConfig
       params:
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
   talstomize build . --validate=false # skip validating the rendered configs
   ```

   Every rendered config is first validated with the same static checks as `talosctl validate --mode metal` - schema errors, a missing install disk, or a setting declared in both the classic v1alpha1 fields and their Talos 1.14 document form. If any node fails, nothing is written. These checks can't see the node itself (e.g. whether the install disk actually exists), so `apply` can still be rejected by the node.

4. Or apply them straight to the nodes:

   ```shell
   talstomize apply -f .                           # every node
   talstomize apply -f . --node nodea              # just one
   talstomize apply -f . -- --insecure             # first apply, maintenance mode
   talstomize apply -f . -- --insecure --dry-run   # multiple flags at once
   ```

5. Or check what would change before applying anything - and, importantly, whether a
   `talosVersion`/`kubernetesVersion`/`schematic` bump you already `apply`'d actually took
   effect:

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
       talos: running v1.13.7, want v1.13.8 (ghcr.io/siderolabs/installer:v1.13.8)
       extensions: missing [qemu-guest-agent]
       kernel args: missing [amdgpu.gttsize=40960]
   ==> nodeb (10.5.0.12): no differences
   ==> kubernetes: running 1.36.2, want 1.36.3
   ```

   Exits `0` if every node matches, `1` if any node differs (or on error) -
   safe to use in a script or CI check.

>[!IMPORTANT]
> `apply` and `diff` are thin wrappers around `talosctl`, and neither one ever runs
> `talosctl upgrade`/`upgrade-k8s` itself - actually upgrading a node's Talos OS or the
> cluster's Kubernetes version is always a deliberate, manual step outside talstomize.
> `apply` renders each node's config the same way `build` does, then runs
> `talosctl apply-config --nodes <ip> --file <rendered>` for it - this reconciles machine
> config, nothing more. `diff` renders the same way and reports every difference between
> that and live cluster state: the running machine config (`talosctl get machineconfig`,
> encoded without comments so the diff isn't swamped by Talos's own per-field
> documentation), the node's actual booted Talos OS version (`talosctl get version` -
> deliberately *not* the machine config's own install image field, which only reflects
> last-declared intent and can already agree with the rendered config even when the node
> was never actually rebooted), and - if `installer.schematic` is set - its installed
> system extensions and kernel args (`talosctl get extensions`/`get cmdline`), plus the
> cluster's running Kubernetes version (`talosctl upgrade-k8s --dry-run`), once, cluster-wide.  
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
| `talosVersion`                 | Optional; the Talos version every node runs (e.g. `v1.14.2`, leading `v` optional). Selects the machine config style the base config is generated in (`talosctl gen config`'s `--talos-version`), and tags every node's installer image. Defaults to the Talos version bundled with talstomize's machinery dependency; a newer one is rejected. Your patches are passed through as-is - they're never converted to match. See [Config style](#config-style). |
| `additionalSubjectAltNames`    | Optional; extra SANs added to both the machine and kube-apiserver certificates on every node (`talosctl gen config`'s `--additional-sans`). |
| `dnsDomain`                    | Optional; the cluster's DNS domain (`talosctl gen config`'s `--dns-domain`). Defaults to `cluster.local`. |
| `installer.schematic`          | Optional; an [Image Factory](https://factory.talos.dev) schematic customization document (`extraKernelArgs`, `systemExtensions`, ...), resolved to a schematic ID and tagged with `talosVersion` into the cluster-wide installer image. Leave it unset for the vanilla (no customization) installer. Only the `metal` platform is supported for now. See [Image Factory schematics](#image-factory-schematics). |
| `nodes.<name>.ip`              | The node's address, used both as an API server SAN input and as the `talosctl` target.          |
| `nodes.<name>.kind`            | `controlplane` or `worker`.                                                                     |
| `nodes.<name>.installer`       | Optional; overrides `installer` for this node only (a full replacement, not merged). Still tagged with the cluster-wide `talosVersion`. |
| `nodes.<name>.patches`         | Patches applied to this node only, after the cluster-wide and role-wide patches.                |
| `patches`                      | Patches applied to every node, regardless of role - the equivalent of `talosctl gen config`'s unprefixed `--config-patch`. |
| `controlplanePatches`          | Patches applied to every `controlplane` node.                                                   |
| `workerPatches`                | Patches applied to every `worker` node.                                                         |

Unknown fields are rejected, so a typo (or a field from an older talstomize.yaml format) fails loudly instead of being silently ignored.

Patches are applied in order: implicit hostname patch (the node's name - a `HostnameConfig` document from Talos 1.12, `machine.network.hostname` before that) → implicit install image patch → `patches` (cluster-wide) → role patches (`controlplanePatches`/`workerPatches`) → node patches, each overriding the ones before it.

### Install image placement

Talos 1.14 moved install settings from the legacy v1alpha1 `machine.install` block into the `UnattendedInstallConfig` document, and rejects a config that uses both. So the installer image goes wherever your patches declared the rest of the install config:

- if a patch defines an `UnattendedInstallConfig` (e.g. with a `provisioning.diskSelector`), the image is set as its `installer.image`;
- otherwise, if a patch sets the legacy `machine.install` block (e.g. `machine.install.disk`), the image is set as `machine.install.image`;
- with neither, it follows `talosVersion`: an `UnattendedInstallConfig` from Talos 1.14, `machine.install` before that.

An image a patch sets explicitly in either place always wins - that's how to use an installer image that doesn't come from the Image Factory.

### Config style

`talosVersion` also picks the *schema* of the rendered config (the "version contract"). Talos 1.14 moved what used to be nested fields (`machine.kubelet`, `machine.features.kubePrism`, `cluster.apiServer`, ...) into standalone documents (`KubeletConfig`, `KubePrismConfig`, `KubeAPIServerConfig`, ...), and the base config is generated in whichever shape matches `talosVersion`. Write your patches in that same shape: Talos 1.14+ rejects a config that sets the same thing both ways, and Talos 1.13 and older can't decode the new documents at all. `build` validates every rendered config, so a mismatch fails at build time rather than at `apply`.

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
document to the [Image Factory](https://factory.talos.dev) and turns the resulting schematic ID, tagged with the cluster-wide `talosVersion`, into the node's installer image:

```yaml
# talstomize.yaml
talosVersion: v1.13.8
installer:
  schematic:
    customization:
      extraKernelArgs:
        - cpufreq.default_governor=performance
      systemExtensions:
        officialExtensions:
          - siderolabs/zfs
          - siderolabs/iscsi-tools
```

This resolves (once per unique customization, memoized - a schematic shared by several nodes only hits the Factory once) to the installer image `factory.talos.dev/metal-installer/<schematic id>:v1.13.8`. `nodes.<name>.installer` overrides it per node, for hardware that needs its own extensions (e.g. one node with a GPU). With no schematic set, nodes get the vanilla installer (`factory.talos.dev/metal-installer/376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba:<talosVersion>`, the same default `talosctl gen config` uses).

>[!NOTE]
> Unlike everything else talstomize does, resolving a schematic needs network access to `factory.talos.dev` - `build`/`apply` will fail if it's unreachable. The vanilla installer's schematic ID is fixed, so leaving `installer` unset entirely avoids this.
