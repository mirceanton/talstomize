// Package source resolves a talstomize.yaml patch reference - a local
// file path or an https URL (e.g. a GitHub raw URL) - to its raw
// contents.
package source

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// Read resolves ref to its raw contents. A ref parsed as an http or https
// URL is fetched over the network (only https is actually allowed - see
// readURL); anything else is treated as a file path, resolved relative to
// dir if not already absolute.
func Read(dir, ref string) ([]byte, error) {
	if isHTTPURL(ref) {
		return readURL(ref)
	}

	path := ref
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	return contents, nil
}

func isHTTPURL(ref string) bool {
	u, err := url.Parse(ref)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// readURL fetches ref over the network. Plain http is rejected rather than
// silently fetched: patch content becomes machine config, and an
// unauthenticated http response can be tampered with in transit.
func readURL(ref string) ([]byte, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ref, err)
	}

	if u.Scheme != "https" {
		return nil, fmt.Errorf("fetching %s: only https URLs are supported, got %q", ref, u.Scheme)
	}

	resp, err := httpClient.Get(ref)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", ref, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: unexpected status %s", ref, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: reading response: %w", ref, err)
	}

	return body, nil
}
