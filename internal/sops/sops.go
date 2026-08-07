// Package sops transparently decrypts sops-encrypted YAML files.
//
// It shells out to the sops CLI (must be on PATH) rather than embedding
// sops as a library: sops' decrypt package unconditionally pulls in every
// key backend it supports (AWS, GCP, Azure, Vault, PGP, age - hundreds of
// transitive packages), which would bloat talstomize's own binary for a
// feature most users won't touch. A plaintext file never shells out to
// anything, so talstomize gains no new dependency unless a file is
// actually encrypted - the same trade-off `apply` already makes by
// shelling out to talosctl.
package sops

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaybeDecrypt reads path and, if its content looks sops-encrypted (has a
// top-level "sops" key - the marker sops itself uses to identify files it
// manages), decrypts it via the sops CLI. Otherwise it returns the file's
// contents unchanged.
func MaybeDecrypt(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if !isEncrypted(raw) {
		return raw, nil
	}

	cmd := exec.Command("sops", "--decrypt", path)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("decrypting %s: sops is required to decrypt this file but wasn't found on PATH", path)
		}

		return nil, fmt.Errorf("decrypting %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}

func isEncrypted(raw []byte) bool {
	var probe struct {
		Sops any `yaml:"sops"`
	}

	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return false
	}

	return probe.Sops != nil
}
