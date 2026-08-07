// Package talos wraps the Talos machinery config generation and patching
// packages to render per-node machine configs from a talstomize.yaml.
package talos

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/constants"

	tstomcfg "github.com/mirceanton/talstomize/internal/config"
	tstomsops "github.com/mirceanton/talstomize/internal/sops"
)

// Engine renders per-node Talos machine configs from a talstomize Config.
type Engine struct {
	cfg   *tstomcfg.Config
	input *generate.Input
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

	input, err := generate.NewInput(cfg.ClusterName, cfg.ControlPlaneEndpoint, kubernetesVersion,
		generate.WithSecretsBundle(bundle),
		generate.WithEndpointList(endpoints),
	)
	if err != nil {
		return nil, fmt.Errorf("preparing config generation: %w", err)
	}

	return &Engine{cfg: cfg, input: input}, nil
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

// RenderNode builds the final, patched machine config for a single node:
// the base role config, with the role-wide patches applied, then the
// node's own patches, with an implicit hostname patch applied first so
// that patches can still override it.
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

	rolePatches := e.cfg.WorkerPatches
	if node.Kind == tstomcfg.KindControlPlane {
		rolePatches = e.cfg.ControlPlanePatches
	}

	resolved, err := ResolvePatches(e.cfg.Dir(), rolePatches)
	if err != nil {
		return nil, fmt.Errorf("node %q: role patches: %w", name, err)
	}

	patches = append(patches, resolved...)

	resolved, err = ResolvePatches(e.cfg.Dir(), node.Patches)
	if err != nil {
		return nil, fmt.Errorf("node %q: patches: %w", name, err)
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
