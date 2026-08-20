package version

import "testing"

func TestStringDefault(t *testing.T) {
	if String() != "dev" {
		t.Fatalf("got %q", String())
	}
}

func TestStringOverride(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "v0.1.6"
	if String() != "0.1.6" {
		t.Fatalf("got %q", String())
	}
}
