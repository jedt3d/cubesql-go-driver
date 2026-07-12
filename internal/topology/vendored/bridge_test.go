package vendored

import "testing"

func TestVersion(t *testing.T) {
	if got, want := Version(), "060600"; got != want {
		t.Fatalf("Version() = %q, want %q", got, want)
	}
}
