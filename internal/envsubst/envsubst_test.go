package envsubst_test

import (
	"strings"
	"testing"

	"github.com/mirceanton/talstomize/internal/envsubst"
)

func TestExpand(t *testing.T) {
	t.Setenv("TALSTOMIZE_TEST_USER", "alice")
	t.Setenv("TALSTOMIZE_TEST_PASS", "s3cr3t")

	in := "username: ${TALSTOMIZE_TEST_USER}\npassword: ${TALSTOMIZE_TEST_PASS}\n"

	out, err := envsubst.Expand([]byte(in))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	want := "username: alice\npassword: s3cr3t\n"
	if string(out) != want {
		t.Errorf("Expand(%q) = %q, want %q", in, out, want)
	}
}

func TestExpandNoReferences(t *testing.T) {
	in := "machine:\n  hostname: nodea\n"

	out, err := envsubst.Expand([]byte(in))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	if string(out) != in {
		t.Errorf("Expand(%q) = %q, want unchanged", in, out)
	}
}

func TestExpandUndefined(t *testing.T) {
	_, err := envsubst.Expand([]byte("password: ${TALSTOMIZE_TEST_UNDEFINED}\n"))
	if err == nil {
		t.Fatal("Expand: expected an error for an undefined variable, got nil")
	}

	if !strings.Contains(err.Error(), "TALSTOMIZE_TEST_UNDEFINED") {
		t.Errorf("Expand error = %q, want it to name the undefined variable", err)
	}
}

func TestExpandUndefinedListsAllMissing(t *testing.T) {
	t.Setenv("TALSTOMIZE_TEST_SET", "ok")

	_, err := envsubst.Expand([]byte("a: ${TALSTOMIZE_TEST_MISSING_A}\nb: ${TALSTOMIZE_TEST_SET}\nc: ${TALSTOMIZE_TEST_MISSING_C}\n"))
	if err == nil {
		t.Fatal("Expand: expected an error, got nil")
	}

	for _, want := range []string{"TALSTOMIZE_TEST_MISSING_A", "TALSTOMIZE_TEST_MISSING_C"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expand error = %q, want it to mention %q", err, want)
		}
	}

	if strings.Contains(err.Error(), "TALSTOMIZE_TEST_SET") {
		t.Errorf("Expand error = %q, should not mention a defined variable", err)
	}
}
