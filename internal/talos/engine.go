// Package talos wraps the Talos machinery config generation and patching
// packages to render per-node machine configs from a talstomize.yaml.
package talos

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/blang/semver/v4"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	tversion "github.com/siderolabs/talos/pkg/machinery/version"

	tstomcfg "github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/factory"
	tstomsops "github.com/mirceanton/talstomize/internal/sops"
)

// Engine renders per-node Talos machine configs from a talstomize Config.
type Engine struct {
	cfg               *tstomcfg.Config
	input             *generate.Input
	talosVersion      string
	contract          *tconfig.VersionContract
	kubernetesVersion string
	controlPlaneIPs   []string

	// resolveSchematic posts a schematic customization to the Image
	// Factory and returns its ID; defaults to factory.Schematic, swappable
	// in tests. schematics memoizes customization YAML -> schematic ID, so
	// a cluster-wide schematic shared by many nodes is only resolved once.
	resolveSchematic func([]byte) (string, error)
	schematics       map[string]string
}

// NewEngine loads the referenced secrets bundle and prepares config
// generation for the given talstomize config.
func NewEngine(cfg *tstomcfg.Config) (*Engine, error) {
	bundle, err := loadSecretsBundle(cfg.SecretsPath())
	if err != nil {
		return nil, fmt.Errorf(
			"loading secrets bundle from %s (generate one with `talosctl gen secrets -o %s`): %w",
			cfg.SecretsPath(), cfg.Secrets, err,
		)
	}

	// Talos machinery builds image references as "<image>:v<KubernetesVersion>", so a
	// leading "v" here (e.g. copy-pasted from a Talhelper config) would double up.
	kubernetesVersion := strings.TrimPrefix(cfg.KubernetesVersion, "v")
	if kubernetesVersion == "" {
		kubernetesVersion = constants.DefaultKubernetesVersion
	}

	// Normalized to always carry a leading "v": it tags the installer image.
	talosVersion := "v" + strings.TrimPrefix(cfg.TalosVersion, "v")
	if cfg.TalosVersion == "" {
		talosVersion = tversion.Tag
	}

	// The version contract picks the config schema the generated base uses:
	// the classic, single-document v1alpha1 shape up to Talos 1.13, the
	// multi-document shape (KubeletConfig, KubePrismConfig, ...) from 1.14
	// on. It follows talosVersion, the version the nodes actually run, so a
	// classic-era node isn't handed documents it can't decode.
	contract, err := tconfig.ParseContractFromVersion(talosVersion)
	if err != nil {
		return nil, fmt.Errorf("talosVersion: parsing talos version %q: %w", talosVersion, err)
	}

	// The machinery can only generate config styles it knows about.
	current, err := tconfig.ParseContractFromVersion(tversion.Tag)
	if err != nil {
		return nil, fmt.Errorf("parsing bundled Talos version %q: %w", tversion.Tag, err)
	}

	if contract.Greater(current) {
		return nil, fmt.Errorf("talosVersion %s is newer than the Talos machinery talstomize was built with (%s)", talosVersion, tversion.Tag)
	}

	if err := checkPlatforms(cfg, talosVersion); err != nil {
		return nil, err
	}

	var controlPlaneIPs []string

	for _, node := range cfg.Nodes {
		if node.Kind == tstomcfg.KindControlPlane {
			controlPlaneIPs = append(controlPlaneIPs, node.IP)
		}
	}

	sort.Strings(controlPlaneIPs)

	opts := []generate.Option{
		generate.WithSecretsBundle(bundle),
		generate.WithEndpointList(controlPlaneIPs),
		generate.WithAdditionalSubjectAltNames(cfg.AdditionalSubjectAltNames),
		generate.WithVersionContract(contract),
	}

	// WithDNSDomain unconditionally overwrites the "cluster.local" default,
	// so it must only be added when actually set.
	if cfg.DNSDomain != "" {
		opts = append(opts, generate.WithDNSDomain(cfg.DNSDomain))
	}

	input, err := generate.NewInput(cfg.ClusterName, cfg.ControlPlaneEndpoint, kubernetesVersion, opts...)
	if err != nil {
		return nil, fmt.Errorf("preparing config generation: %w", err)
	}

	return &Engine{
		cfg:               cfg,
		input:             input,
		talosVersion:      talosVersion,
		contract:          contract,
		kubernetesVersion: kubernetesVersion,
		controlPlaneIPs:   controlPlaneIPs,
		resolveSchematic:  factory.Schematic,
	}, nil
}

// checkPlatforms rejects a node whose platform only exists from a newer
// Talos version than talosVersion.
func checkPlatforms(cfg *tstomcfg.Config, talosVersion string) error {
	version, err := semver.ParseTolerant(talosVersion)
	if err != nil {
		return fmt.Errorf("talosVersion: parsing %q: %w", talosVersion, err)
	}

	for _, name := range slices.Sorted(maps.Keys(cfg.Nodes)) {
		platform, ok := tstomcfg.LookupPlatform(cfg.NodePlatform(cfg.Nodes[name]))
		if ok && version.LT(platform.MinVersion) {
			return fmt.Errorf("node %q: platform %q requires Talos %s or newer, but talosVersion is %s",
				name, platform.Name, platform.MinVersion, talosVersion)
		}
	}

	return nil
}

// KubernetesVersion returns the Kubernetes version every node's config is
// rendered against (Config.KubernetesVersion, defaulted to
// constants.DefaultKubernetesVersion when unset, with any leading "v"
// stripped) - the same value already baked into every rendered machine
// config, not a second source of truth.
func (e *Engine) KubernetesVersion() string {
	return e.kubernetesVersion
}

// ControlPlaneIPs returns the sorted IPs of every controlplane node in the
// cluster. Cluster-wide talosctl calls that have no single node of their
// own to target (e.g. `upgrade-k8s`, which otherwise fails with "nodes are
// not set for the command") use this as their `--nodes` entry point.
func (e *Engine) ControlPlaneIPs() []string {
	return e.controlPlaneIPs
}

// Talosconfig returns the talosctl client configuration for the cluster,
// authenticated against the same secrets bundle used to generate machine
// configs - the equivalent of talosctl gen config's default "talosconfig"
// output, with admin ("os:admin") access.
func (e *Engine) Talosconfig() (*clientconfig.Config, error) {
	return e.input.Talosconfig()
}

// loadSecretsBundle reads the secrets bundle at path, transparently
// decrypting it first if it's a sops-encrypted file, so a `talosctl gen
// secrets` bundle can be committed to git the same way as any other
// sops-managed secret.
func loadSecretsBundle(path string) (*secrets.Bundle, error) {
	raw, err := tstomsops.MaybeDecrypt(path)
	if err != nil {
		return nil, err
	}

	bundle := &secrets.Bundle{Clock: secrets.NewClock()}
	if err := yaml.Unmarshal(raw, bundle); err != nil {
		return nil, err
	}

	return bundle, nil
}

// effectiveInstaller returns the installer config effective for node: its
// own installer if it sets a schematic (a full replacement, not merged),
// else the cluster-wide one.
func (e *Engine) effectiveInstaller(node tstomcfg.Node) tstomcfg.Installer {
	if node.Installer.SchematicSet() {
		return node.Installer
	}

	return e.cfg.Installer
}

// InstallImage resolves the installer image talstomize computes for node:
// its platform's Image Factory installer for its effective schematic (or
// the vanilla one, if none is set), tagged with the cluster's talosVersion.
func (e *Engine) InstallImage(node tstomcfg.Node) (string, error) {
	id := factory.VanillaSchematicID

	if installer := e.effectiveInstaller(node); installer.SchematicSet() {
		var err error

		id, err = e.resolveSchematicID(installer.Schematic)
		if err != nil {
			return "", err
		}
	}

	return factory.InstallerImage(e.cfg.NodePlatform(node), id, e.talosVersion), nil
}

// Platform returns the Talos platform node runs on.
func (e *Engine) Platform(node tstomcfg.Node) string {
	return e.cfg.NodePlatform(node)
}

// EffectiveCustomization returns the parsed schematic customization (extra
// kernel args + desired system extensions) effective for node, with the
// same precedence as InstallImage - empty for the vanilla schematic.
func (e *Engine) EffectiveCustomization(node tstomcfg.Node) (factory.Customization, error) {
	var customization factory.Customization

	installer := e.effectiveInstaller(node)
	if !installer.SchematicSet() {
		return customization, nil
	}

	if err := installer.Schematic.Decode(&customization); err != nil {
		return factory.Customization{}, fmt.Errorf("parsing schematic: %w", err)
	}

	return customization, nil
}

// resolveSchematicID posts schematic to the Image Factory (memoized, so the
// same customization is only ever resolved once) and returns its ID.
func (e *Engine) resolveSchematicID(schematic yaml.Node) (string, error) {
	raw, err := yaml.Marshal(&schematic)
	if err != nil {
		return "", fmt.Errorf("marshaling schematic: %w", err)
	}

	id, ok := e.schematics[string(raw)]
	if !ok {
		id, err = e.resolveSchematic(raw)
		if err != nil {
			return "", fmt.Errorf("resolving schematic: %w", err)
		}

		if e.schematics == nil {
			e.schematics = map[string]string{}
		}

		e.schematics[string(raw)] = id
	}

	return id, nil
}

// RenderNode builds the final, patched machine config for a single node, in
// order: implicit hostname patch → implicit install image patch (if set) →
// cluster-wide patches → role patches → node's own patches, each overriding
// the ones before it.
func (e *Engine) RenderNode(name string) (tconfig.Provider, error) {
	node, ok := e.cfg.Nodes[name]
	if !ok {
		return nil, fmt.Errorf("unknown node %q", name)
	}

	nodeType, err := machine.ParseType(node.Kind)
	if err != nil {
		return nil, fmt.Errorf("node %q: %w", name, err)
	}

	base, err := e.input.Config(nodeType)
	if err != nil {
		return nil, fmt.Errorf("generating base config for node %q: %w", name, err)
	}

	// From Talos 1.12, the base config carries a default `HostnameConfig{auto: stable}`
	// document; switch it to a static hostname (Talos rejects `auto` and `hostname`
	// being set together). Older versions only have machine.network.hostname.
	hostnameFormat := "machine:\n  network:\n    hostname: %s\n"
	if e.contract.MultidocNetworkConfigSupported() {
		hostnameFormat = "apiVersion: v1alpha1\nkind: HostnameConfig\nauto: \"off\"\nhostname: %s\n"
	}

	hostnamePatch, err := configpatcher.LoadPatch(fmt.Appendf(nil, hostnameFormat, name))
	if err != nil {
		return nil, fmt.Errorf("node %q: building hostname patch: %w", name, err)
	}

	userPatches, err := e.userPatches(node)
	if err != nil {
		return nil, fmt.Errorf("node %q: %w", name, err)
	}

	rendered, err := applyPatches(base, append([]configpatcher.Patch{hostnamePatch}, userPatches...))
	if err != nil {
		return nil, fmt.Errorf("node %q: applying patches: %w", name, err)
	}

	installImage, err := e.InstallImage(node)
	if err != nil {
		return nil, fmt.Errorf("node %q: resolving install image: %w", name, err)
	}

	// Where the install image patch goes depends on what the user's patches did,
	// so it's only known after a first render. It's then re-rendered with the image
	// patch placed *before* the user's patches rather than merged on afterwards:
	// merging any UnattendedInstallConfig patch without a diskSelector onto one that
	// has it wipes it (cel.Expression.Merge copies the right side unconditionally).
	imagePatchFormat, ok := installImagePatch(rendered, e.contract, e.Platform(node) == tstomcfg.PlatformMetal)
	if !ok {
		return rendered, nil
	}

	imagePatch, err := configpatcher.LoadPatch(fmt.Appendf(nil, imagePatchFormat, installImage))
	if err != nil {
		return nil, fmt.Errorf("node %q: building install image patch: %w", name, err)
	}

	rendered, err = applyPatches(base, append([]configpatcher.Patch{hostnamePatch, imagePatch}, userPatches...))
	if err != nil {
		return nil, fmt.Errorf("node %q: applying patches: %w", name, err)
	}

	return rendered, nil
}

// userPatches resolves node's patches from the talstomize config, in order:
// cluster-wide, role, then the node's own.
func (e *Engine) userPatches(node tstomcfg.Node) ([]configpatcher.Patch, error) {
	rolePatches := e.cfg.WorkerPatches
	if node.Kind == tstomcfg.KindControlPlane {
		rolePatches = e.cfg.ControlPlanePatches
	}

	var patches []configpatcher.Patch

	for _, group := range []struct {
		desc string
		refs []yaml.Node
	}{
		{"cluster patches", e.cfg.Patches},
		{"role patches", rolePatches},
		{"node patches", node.Patches},
	} {
		resolved, err := ResolvePatches(e.cfg.Dir(), group.refs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", group.desc, err)
		}

		patches = append(patches, resolved...)
	}

	return patches, nil
}

func applyPatches(base tconfig.Provider, patches []configpatcher.Patch) (tconfig.Provider, error) {
	out, err := configpatcher.Apply(configpatcher.WithConfig(base), patches)
	if err != nil {
		return nil, err
	}

	return out.Config()
}

// installImagePatch returns the patch format string (with a single %s for
// the image) that sets the install image wherever rendered declares the rest
// of its install config - an UnattendedInstallConfig document, or the legacy
// v1alpha1 machine.install block - since Talos rejects a config that uses
// both. With neither present, it follows contract: an UnattendedInstallConfig
// document from Talos 1.14, machine.install before that - except that a
// node which isn't installed to disk (any non-metal platform, booted from
// the provider's disk image) gets no UnattendedInstallConfig of its own,
// since Talos rejects one without a disk selector. ok is false when no
// image patch is needed, including when a patch already set an install
// image explicitly, which always wins.
func installImagePatch(rendered tconfig.Provider, contract *tconfig.VersionContract, installed bool) (format string, ok bool) {
	const (
		unattended = "apiVersion: v1alpha1\nkind: UnattendedInstallConfig\ninstaller:\n  image: %s\n"
		legacy     = "machine:\n  install:\n    image: %s\n"
	)

	if InstallImage(rendered) != "" {
		return "", false
	}

	switch {
	case rendered.UnattendedInstallConfig() != nil:
		return unattended, true
	case legacyInstall(rendered) != nil, !contract.UnattendedInstallConfig():
		return legacy, true
	case !installed:
		return "", false
	default:
		return unattended, true
	}
}

// legacyInstall returns cfg's deprecated v1alpha1 machine.install block, or
// nil if it isn't set.
func legacyInstall(cfg tconfig.Provider) *v1alpha1.InstallConfig { //nolint:staticcheck // detecting legacy configuration
	raw := cfg.RawV1Alpha1()
	if raw == nil || raw.MachineConfig == nil {
		return nil
	}

	return raw.MachineConfig.MachineInstall //nolint:staticcheck // detecting legacy configuration
}

// InstallImage returns the install image cfg declares, from its
// UnattendedInstallConfig document if present, else the legacy v1alpha1
// machine.install block - "" if neither sets one.
func InstallImage(cfg tconfig.Provider) string {
	if unattended := cfg.UnattendedInstallConfig(); unattended != nil {
		return unattended.InstallerImage()
	}

	if legacy := legacyInstall(cfg); legacy != nil {
		return legacy.InstallImage
	}

	return ""
}

// NodeNames returns the sorted list of node names in the config.
func (e *Engine) NodeNames(filter string) []string {
	names := make([]string, 0, len(e.cfg.Nodes))

	for name := range e.cfg.Nodes {
		if filter != "" && name != filter {
			continue
		}

		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// Node returns the raw node definition for name.
func (e *Engine) Node(name string) (tstomcfg.Node, bool) {
	node, ok := e.cfg.Nodes[name]

	return node, ok
}
