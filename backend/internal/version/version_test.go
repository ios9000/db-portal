package version

import "testing"

func TestVersion(t *testing.T) {
	if Version != "0.0.1" {
		t.Fatalf("unexpected version %q", Version)
	}
}
