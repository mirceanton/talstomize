package sops_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirceanton/talstomize/internal/sops"
)

func TestMaybeDecryptPlaintextPassesThrough(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.yaml")

	const contents = "hello: world\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	out, err := sops.MaybeDecrypt(path)
	if err != nil {
		t.Fatalf("MaybeDecrypt: %v", err)
	}

	if string(out) != contents {
		t.Errorf("MaybeDecrypt(%s) = %q, want unchanged %q", path, out, contents)
	}
}

// TestMaybeDecryptRoundTrip decrypts testdata/encrypted.yaml - encrypted
// for a disposable, checked-in age key (testdata/age-key.txt, used for
// nothing else) - and checks the plaintext comes back out. Requires sops
// on PATH; skipped otherwise (see .mise.toml, where it's pinned for CI).
func TestMaybeDecryptRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("sops"); err != nil {
		t.Skip("sops not found on PATH")
	}

	t.Setenv("SOPS_AGE_KEY_FILE", must(filepath.Abs("testdata/age-key.txt")))

	out, err := sops.MaybeDecrypt("testdata/encrypted.yaml")
	if err != nil {
		t.Fatalf("MaybeDecrypt: %v", err)
	}

	for _, want := range []string{"hello: world", `value: "42"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("MaybeDecrypt output = %q, want it to contain %q", out, want)
		}
	}
}

func TestMaybeDecryptMissingSops(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "encrypted.yaml")

	const contents = "hello: ENC[AES256_GCM,data:xxx,iv:xxx,tag:xxx,type:str]\nsops:\n    version: 3.13.3\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	// An empty PATH guarantees sops can't be found, regardless of the host.
	t.Setenv("PATH", "")

	if _, err := sops.MaybeDecrypt(path); err == nil {
		t.Fatal("MaybeDecrypt: expected an error when sops isn't on PATH, got nil")
	} else if !strings.Contains(err.Error(), "PATH") {
		t.Errorf("MaybeDecrypt error = %q, want it to mention PATH", err)
	}
}

func TestMaybeDecryptMissingFile(t *testing.T) {
	if _, err := sops.MaybeDecrypt(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatal("MaybeDecrypt: expected an error, got nil")
	}
}

func must(s string, err error) string {
	if err != nil {
		panic(err)
	}

	return s
}
