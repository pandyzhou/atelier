package secret

import (
	"strings"
	"testing"
)

func TestSealAndOpen(t *testing.T) {
	b := New(t.TempDir())
	const key = "sk-test-0123456789 密钥"
	sealed, err := b.Seal(key)
	if err != nil {
		t.Fatal(err)
	}
	if sealed == "" || strings.Contains(sealed, "sk-test") || strings.Contains(sealed, "密钥") {
		t.Fatalf("sealed text shows the secret: %q", sealed)
	}
	got, err := b.Open(sealed)
	if err != nil || got != key {
		t.Fatalf("opened %q, %v; want %q", got, err, key)
	}
	// Sealing the same twice gives different text: nothing tells that two
	// saved keys are the same.
	again, _ := b.Seal(key)
	if again == sealed {
		t.Errorf("sealing twice gave the same text")
	}
}

func TestEmptyAndForeign(t *testing.T) {
	b := New(t.TempDir())
	if s, err := b.Seal(""); s != "" || err != nil {
		t.Errorf("sealing nothing: %q, %v", s, err)
	}
	if s, err := b.Open(""); s != "" || err != nil {
		t.Errorf("opening nothing: %q, %v", s, err)
	}
	for _, bad := range []string{"sk-plain-text", "other:AAAA", scheme + ":not base64!", scheme + ":AAAA"} {
		if s, err := b.Open(bad); err == nil {
			t.Errorf("opening %q gave %q without an error", bad, s)
		}
	}
}
