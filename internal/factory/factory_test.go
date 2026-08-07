package factory

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	origURL, origClient := baseURL, httpClient
	baseURL = server.URL
	httpClient = server.Client()

	t.Cleanup(func() {
		baseURL, httpClient = origURL, origClient
	})
}

func TestSchematic(t *testing.T) {
	const customization = "customization:\n  extraKernelArgs: [foo]\n"

	var gotBody []byte

	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		// The real Factory returns 201, not 200 - match that so this test
		// would have caught the status-code check bug that shipped here.
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "abc123", "schematic": customization})
	})

	id, err := Schematic([]byte(customization))
	if err != nil {
		t.Fatalf("Schematic: %v", err)
	}

	if id != "abc123" {
		t.Errorf("Schematic() = %q, want %q", id, "abc123")
	}

	if string(gotBody) != customization {
		t.Errorf("posted body = %q, want the customization unchanged: %q", gotBody, customization)
	}
}

func TestSchematicErrorStatus(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad schematic", http.StatusBadRequest)
	})

	if _, err := Schematic([]byte("bogus")); err == nil {
		t.Fatal("Schematic: expected an error for a non-200 response, got nil")
	}
}

func TestSchematicMissingID(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{})
	})

	_, err := Schematic([]byte("customization: {}"))
	if err == nil {
		t.Fatal("Schematic: expected an error for a response missing id, got nil")
	}

	if !strings.Contains(err.Error(), "missing id") {
		t.Errorf("Schematic error = %q, want it to mention the missing id", err)
	}
}

func TestSchematicUnreachable(t *testing.T) {
	origURL := baseURL
	baseURL = "http://127.0.0.1:1" // nothing listens here
	t.Cleanup(func() { baseURL = origURL })

	if _, err := Schematic([]byte("customization: {}")); err == nil {
		t.Fatal("Schematic: expected an error when the Factory is unreachable, got nil")
	}
}

func TestInstallerImage(t *testing.T) {
	got := InstallerImage("abc123", "v1.13.8")
	want := "factory.talos.dev/metal-installer/abc123:v1.13.8"

	if got != want {
		t.Errorf("InstallerImage() = %q, want %q", got, want)
	}
}
