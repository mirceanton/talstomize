// Package config defines and loads the talstomize.yaml configuration format.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"gopkg.in/yaml.v3"

	"github.com/mirceanton/talstomize/internal/envsubst"
)

// APIVersion and Kind are the only accepted values for the corresponding
// top-level fields of a talstomize.yaml.
const (
	APIVersion = "config.talstomize.dev/v1alpha1"
	Kind       = "Talstomize"
)

// Valid values for Node.Kind.
const (
	KindControlPlane = "controlplane"
	KindWorker       = "worker"
)

// Node describes a single Talos node and the patches that apply only to it.
type Node struct {
	IP      string      `yaml:"ip"`
	Kind    string      `yaml:"kind"`
	Patches []yaml.Node `yaml:"patches,omitempty"`

	// Installer overrides Config.Installer for this node only - a full
	// replacement, not merged.
	Installer Installer `yaml:"installer,omitempty"`
}

// Config is the parsed content of a talstomize.yaml file.
type Config struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`

	ClusterName          string `yaml:"clusterName"`
	ControlPlaneEndpoint string `yaml:"controlPlaneEndpoint"`
	KubernetesVersion    string `yaml:"kubernetesVersion,omitempty"`
	Secrets              string `yaml:"secrets"`

	// TalosVersion is the Talos version every node runs (e.g. "v1.14.2").
	// It selects the machine config style generated for the base config
	// (talosctl gen config's --talos-version contract) and tags every node's
	// installer image. Optional; defaults to the Talos version bundled with
	// talstomize's machinery dependency.
	TalosVersion string `yaml:"talosVersion,omitempty"`

	// AdditionalSubjectAltNames are added to both the machine and the
	// kube-apiserver certificates, on every node - the equivalent of
	// talosctl gen config's --additional-sans.
	AdditionalSubjectAltNames []string `yaml:"additionalSubjectAltNames,omitempty"`

	// DNSDomain is the equivalent of talosctl gen config's --dns-domain.
	// Optional; defaults to "cluster.local".
	DNSDomain string `yaml:"dnsDomain,omitempty"`

	// Installer configures the cluster-wide default installer image,
	// overridable per node via Node.Installer.
	Installer Installer `yaml:"installer,omitempty"`

	Nodes map[string]Node `yaml:"nodes"`

	// Patches apply to every node regardless of role, before
	// ControlPlanePatches/WorkerPatches - the talstomize.yaml equivalent of
	// talosctl gen config's unprefixed --config-patch.
	Patches             []yaml.Node `yaml:"patches,omitempty"`
	ControlPlanePatches []yaml.Node `yaml:"controlplanePatches,omitempty"`
	WorkerPatches       []yaml.Node `yaml:"workerPatches,omitempty"`

	// dir is the directory the config file was loaded from. Relative paths
	// in the config (secrets, patch files) are resolved against it.
	dir string
}

// Installer configures a node's installer image: an Image Factory
// (factory.talos.dev) Schematic customization document (extraKernelArgs,
// systemExtensions, ...), resolved to a schematic ID and tagged with
// Config.TalosVersion. Leaving Schematic unset uses the vanilla (no
// customization) schematic. Only the "metal" platform is supported for now.
type Installer struct {
	// Schematic is the customization document. A zero Kind (SchematicSet
	// returns false) means unset - decoding into *yaml.Node instead of
	// yaml.Node silently loses the node's content, so this must stay a
	// value, not a pointer.
	Schematic yaml.Node `yaml:"schematic,omitempty"`
}

// SchematicSet reports whether i has a schematic set.
func (i Installer) SchematicSet() bool {
	return i.Schematic.Kind != 0
}

// Load reads and validates a talstomize config. path may point directly at a
// YAML file, or at a directory containing a "talstomize.yaml" file.
func Load(path string) (*Config, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	if stat.IsDir() {
		path = filepath.Join(path, "talstomize.yaml")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", path, err)
	}

	dir := filepath.Dir(abs)

	if err := loadDotEnv(dir); err != nil {
		return nil, err
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	contents, err = envsubst.Expand(contents)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	// Unknown fields are rejected rather than silently ignored, so a typo -
	// or a field from an older talstomize.yaml format - fails loudly.
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)

	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	cfg.dir = dir

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return &cfg, nil
}

// loadDotEnv loads a ".env" file next to talstomize.yaml (in dir) into the
// process environment, if one exists, so envsubst.Expand can pick up its
// values without the caller having to export them first. Like godotenv's
// own Load, it never overrides a variable that's already set - an
// explicitly exported value (or one injected by e.g. `op run`) always
// wins over the .env file.
func loadDotEnv(dir string) error {
	path := filepath.Join(dir, ".env")

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("reading %s: %w", path, err)
	}

	if err := godotenv.Load(path); err != nil {
		return fmt.Errorf("loading %s: %w", path, err)
	}

	return nil
}

// Dir returns the directory the config was loaded from.
func (c *Config) Dir() string {
	return c.dir
}

// SecretsPath returns the absolute path to the referenced secrets bundle.
func (c *Config) SecretsPath() string {
	return c.resolve(c.Secrets)
}

func (c *Config) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(c.dir, path)
}

func (c *Config) validate() error {
	var errs []string

	if c.APIVersion != APIVersion {
		errs = append(errs, fmt.Sprintf("apiVersion must be %q, got %q", APIVersion, c.APIVersion))
	}

	if c.Kind != Kind {
		errs = append(errs, fmt.Sprintf("kind must be %q, got %q", Kind, c.Kind))
	}

	if c.ClusterName == "" {
		errs = append(errs, "clusterName is required")
	}

	if c.ControlPlaneEndpoint == "" {
		errs = append(errs, "controlPlaneEndpoint is required")
	}

	if c.Secrets == "" {
		errs = append(errs, "secrets is required")
	}

	if len(c.Nodes) == 0 {
		errs = append(errs, "at least one node is required")
	}

	if c.TalosVersion != "" {
		if _, err := tconfig.ParseContractFromVersion(c.TalosVersion); err != nil {
			errs = append(errs, fmt.Sprintf("talosVersion: %v", err))
		}
	}

	for name, node := range c.Nodes {
		if node.IP == "" {
			errs = append(errs, fmt.Sprintf("node %q: ip is required", name))
		}

		switch node.Kind {
		case KindControlPlane, KindWorker:
		default:
			errs = append(errs, fmt.Sprintf("node %q: kind must be %q or %q, got %q", name, KindControlPlane, KindWorker, node.Kind))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid config:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}
