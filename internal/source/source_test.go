package source

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTestClient(t *testing.T, client *http.Client) {
	t.Helper()

	orig := httpClient
	httpClient = client
	t.Cleanup(func() { httpClient = orig })
}

func TestReadLocalFileRelative(t *testing.T) {
	dir := t.TempDir()

	const contents = "machine:\n  hostname: nodea\n"
	if err := os.WriteFile(filepath.Join(dir, "patch.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatalf("writing patch.yaml: %v", err)
	}

	got, err := Read(dir, "patch.yaml")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if string(got) != contents {
		t.Errorf("Read = %q, want %q", got, contents)
	}
}

func TestReadLocalFileAbsolute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "patch.yaml")

	const contents = "machine:\n  hostname: nodea\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	// dir is deliberately unrelated: an absolute ref must not be joined onto it.
	got, err := Read("/nowhere", path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if string(got) != contents {
		t.Errorf("Read = %q, want %q", got, contents)
	}
}

func TestReadLocalFileMissing(t *testing.T) {
	if _, err := Read(t.TempDir(), "does-not-exist.yaml"); err == nil {
		t.Fatal("Read: expected an error, got nil")
	}
}

func TestReadURL(t *testing.T) {
	const contents = "machine:\n  hostname: from-url\n"

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(contents))
	}))
	defer server.Close()

	withTestClient(t, server.Client())

	got, err := Read("", server.URL+"/patches/foo.yaml")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if string(got) != contents {
		t.Errorf("Read = %q, want %q", got, contents)
	}
}

func TestReadURLNotFound(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	withTestClient(t, server.Client())

	if _, err := Read("", server.URL); err == nil {
		t.Fatal("Read: expected an error for a 404 response, got nil")
	}
}

func TestReadURLPlainHTTPRejected(t *testing.T) {
	if _, err := Read("", "http://example.com/patch.yaml"); err == nil {
		t.Fatal("Read: expected an error for a plain http:// URL, got nil")
	} else if !strings.Contains(err.Error(), "https") {
		t.Errorf("Read error = %q, want it to mention https", err)
	}
}
