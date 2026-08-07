// Package talos wraps the Talos machinery config generation and patching
// packages to render per-node machine configs from a talstomize.yaml.
package talos

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	tversion "github.com/siderolabs/talos/pkg/machinery/version"

	tstomcfg "github.com/mirceanton/talstomize/internal/config"
	"github.com/mirceanton/talstomize/internal/factory"
	tstomsops "github.com/mirceanton/talstomize/internal/sops"
)

// Engine renders per-node Talos machine configs from a talstomize Config.
type Engine struct {
	cfg          *tstomcfg.Config
	input        *generate.Input
	talosVersion string

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

	var endpoints []string

	for _, node := range cfg.Nodes {
		if node.Kind == tstomcfg.KindControlPlane {
			endpoints = append(endpoints, node.IP)
		}
	}

	opts := []generate.Option{
		generate.WithSecretsBundle(bundle),
		generate.WithEndpointList(endpoints),
		generate.WithAdditionalSubjectAltNames(cfg.AdditionalSubjectAltNames),
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

	// Only consulted for a Schematic-computed install image, so - unlike
	// KubernetesVersion - a leading "v" isn't stripped, it's normalized to
	// always be present: the installer image tag needs it either way.
	talosVersion := "v" + strings.TrimPrefix(cfg.Installer.TalosVersion, "v")
	if cfg.Installer.TalosVersion == "" {
		talosVersion = tversion.Tag
	}

	return &Engine{cfg: cfg, input: input, talosVersion: talosVersion, resolveSchematic: factory.Schematic}, nil
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

// effectiveInstallImage resolves the install image for node: its own
// installer.image or installer.schematic if set (config.validate already
// rejects both being set at once), else the cluster-wide installer.image
// or installer.schematic, else "" - left for patches to set, same as when
// neither is configured.
func (e *Engine) effectiveInstallImage(node tstomcfg.Node) (string, error) {
	switch {
	case node.Installer.Image != "":
		return node.Installer.Image, nil
	case node.Installer.SchematicSet():
		return e.resolveSchematicImage(node.Installer.Schematic)
	case e.cfg.Installer.Image != "":
		return e.cfg.Installer.Image, nil
	case e.cfg.Installer.SchematicSet():
		return e.resolveSchematicImage(e.cfg.Installer.Schematic)
	default:
		return "", nil
	}
}

// resolveSchematicImage posts schematic to the Image Factory (memoized, so
// the same customization is only ever resolved once) and returns the
// resulting metal-platform installer image reference.
func (e *Engine) resolveSchematicImage(schematic yaml.Node) (string, error) {
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

	return factory.InstallerImage(id, e.talosVersion), nil
}

// RenderNode builds the final, patched machine config for a single node, in
// order: implicit hostname patch → implicit per-node install image patch
// (if set) → cluster-wide patches → role patches → node's own patches,
// each overriding the ones before it.
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

	// The base config carries a default `HostnameConfig{auto: stable}` document, which
	// conflicts with setting the legacy machine.network.hostname field below (Talos
	// rejects a config that sets both). Delete it so the static hostname wins.
	hostnamePatch, err := configpatcher.LoadPatch(fmt.Appendf(nil,
		"machine:\n  network:\n    hostname: %s\n---\napiVersion: v1alpha1\nkind: HostnameConfig\n$patch: delete\n",
		name,
	))
	if err != nil {
		return nil, fmt.Errorf("node %q: building hostname patch: %w", name, err)
	}

	patches := []configpatcher.Patch{hostnamePatch}

	installImage, err := e.effectiveInstallImage(node)
	if err != nil {
		return nil, fmt.Errorf("node %q: resolving install image: %w", name, err)
	}

	if installImage != "" {
		installImagePatch, err := configpatcher.LoadPatch(fmt.Appendf(nil,
			"machine:\n  install:\n    image: %s\n", installImage,
		))
		if err != nil {
			return nil, fmt.Errorf("node %q: building install image patch: %w", name, err)
		}

		patches = append(patches, installImagePatch)
	}

	resolved, err := ResolvePatches(e.cfg.Dir(), e.cfg.Patches)
	if err != nil {
		return nil, fmt.Errorf("node %q: cluster patches: %w", name, err)
	}

	patches = append(patches, resolved...)

	rolePatches := e.cfg.WorkerPatches
	if node.Kind == tstomcfg.KindControlPlane {
		rolePatches = e.cfg.ControlPlanePatches
	}

	resolved, err = ResolvePatches(e.cfg.Dir(), rolePatches)
	if err != nil {
		return nil, fmt.Errorf("node %q: role patches: %w", name, err)
	}

	patches = append(patches, resolved...)

	resolved, err = ResolvePatches(e.cfg.Dir(), node.Patches)
	if err != nil {
		return nil, fmt.Errorf("node %q: node patches: %w", name, err)
	}

	patches = append(patches, resolved...)

	out, err := configpatcher.Apply(configpatcher.WithConfig(base), patches)
	if err != nil {
		return nil, fmt.Errorf("node %q: applying patches: %w", name, err)
	}

	return out.Config()
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
