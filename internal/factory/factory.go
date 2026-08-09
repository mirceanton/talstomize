// Package factory resolves Talos Image Factory (https://factory.talos.dev)
// schematics into installer image references.
//
// Only the "metal" platform is supported for now - it's what a bare-metal
// homelab node needs (matching factory.talos.dev's "current" URL format,
// "<platform>-installer/<version>" served for a given schematic), and is
// the common case for talstomize's intended usage. Cloud-platform images
// (aws-installer, gcp-installer, ...) aren't handled.
package factory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	baseURL    = "https://factory.talos.dev"
	httpClient = &http.Client{Timeout: 30 * time.Second}
)

// Customization is the subset of a schematic customization document
// talstomize reads back out (e.g. to compare against what's actually
// installed on a node), rather than just posting opaquely to the Factory.
type Customization struct {
	Customization struct {
		ExtraKernelArgs  []string `yaml:"extraKernelArgs"`
		SystemExtensions struct {
			OfficialExtensions []string `yaml:"officialExtensions"`
		} `yaml:"systemExtensions"`
	} `yaml:"customization"`
}

// Schematic posts a schematic customization document (extraKernelArgs,
// systemExtensions, ...) to the Image Factory and returns the resulting
// schematic ID. The same customization always produces the same ID - the
// Factory computes it as a hash of the (canonicalized) input.
func Schematic(customization []byte) (string, error) {
	resp, err := httpClient.Post(baseURL+"/schematics", "application/x-www-form-urlencoded", bytes.NewReader(customization))
	if err != nil {
		return "", fmt.Errorf("requesting schematic: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading schematic response: %w", err)
	}

	// The Factory returns 201 Created on success, not 200 - accept any 2xx.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("requesting schematic: unexpected status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parsing schematic response: %w", err)
	}

	if parsed.ID == "" {
		return "", fmt.Errorf("schematic response missing id: %s", strings.TrimSpace(string(body)))
	}

	return parsed.ID, nil
}

// InstallerImage builds the metal-platform installer image reference for a
// resolved schematic ID and Talos version, e.g.
// "factory.talos.dev/metal-installer/<id>:v1.13.8".
func InstallerImage(schematicID, talosVersion string) string {
	return fmt.Sprintf("factory.talos.dev/metal-installer/%s:%s", schematicID, talosVersion)
}
