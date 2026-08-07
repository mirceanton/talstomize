package talos

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"

	"github.com/mirceanton/talstomize/internal/envsubst"
)

// ResolvePatches converts raw YAML patch entries from a talstomize.yaml into
// configpatcher patches.
//
// A scalar string entry is treated as a path to a patch file (strategic
// merge or JSON6902), resolved relative to dir. Any other entry (a mapping
// or sequence) is treated as an inline patch document. Either way, the
// content is run through envsubst.Expand before being parsed.
func ResolvePatches(dir string, entries []yaml.Node) ([]configpatcher.Patch, error) {
	patches := make([]configpatcher.Patch, 0, len(entries))

	for i, entry := range entries {
		var raw []byte

		if entry.Kind == yaml.ScalarNode {
			var path string

			if err := entry.Decode(&path); err != nil {
				return nil, fmt.Errorf("patch %d: %w", i, err)
			}

			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}

			contents, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("patch %d: reading %s: %w", i, path, err)
			}

			raw = contents
		} else {
			marshaled, err := yaml.Marshal(&entry)
			if err != nil {
				return nil, fmt.Errorf("patch %d: %w", i, err)
			}

			raw = marshaled
		}

		expanded, err := envsubst.Expand(raw)
		if err != nil {
			return nil, fmt.Errorf("patch %d: %w", i, err)
		}

		patch, err := configpatcher.LoadPatch(expanded)
		if err != nil {
			return nil, fmt.Errorf("patch %d: %w", i, err)
		}

		patches = append(patches, patch)
	}

	return patches, nil
}
