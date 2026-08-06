// Package config defines and loads the talstomize.yaml configuration format.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
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
}

// Config is the parsed content of a talstomize.yaml file.
type Config struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`

	ClusterName          string `yaml:"clusterName"`
	ControlPlaneEndpoint string `yaml:"controlPlaneEndpoint"`
	KubernetesVersion    string `yaml:"kubernetesVersion,omitempty"`
	Secrets              string `yaml:"secrets"`

	Nodes map[string]Node `yaml:"nodes"`

	ControlPlanePatches []yaml.Node `yaml:"controlplanePatches,omitempty"`
	WorkerPatches       []yaml.Node `yaml:"workerPatches,omitempty"`

	// dir is the directory the config file was loaded from. Relative paths
	// in the config (secrets, patch files) are resolved against it.
	dir string
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

	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(contents, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", path, err)
	}

	cfg.dir = filepath.Dir(abs)

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return &cfg, nil
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
