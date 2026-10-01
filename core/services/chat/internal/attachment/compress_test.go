package attachment

import (
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestDocumentsAreCompressedOnlyWhenItPays(t *testing.T) {
	dir := t.TempDir()

	text := filepath.Join(dir, "report.csv")
	if err := os.WriteFile(text, []byte(strings.Repeat("date,amount,comment\n2026-10-01,42,ok\n", 20000)), 0o600); err != nil {
		t.Fatal(err)
	}
	random := filepath.Join(dir, "archive.zip")
	noise := make([]byte, 256<<10)
	if _, err := rand.Read(noise); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(random, noise, 0o600); err != nil {
		t.Fatal(err)
	}

	packed, encoding, err := compressIfWorthIt(text)
	if err != nil {
		t.Fatalf("compress text: %v", err)
	}
	if encoding != "br" || packed == text {
		t.Fatalf("text came back as %q (%q), want a brotli copy", packed, encoding)
	}
	original, _ := os.ReadFile(text)
	f, err := os.Open(packed)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	unpacked, err := io.ReadAll(brotli.NewReader(f))
	if err != nil || string(unpacked) != string(original) {
		t.Fatalf("the brotli copy does not unpack to the original: %v", err)
	}

	same, encoding, err := compressIfWorthIt(random)
	if err != nil || same != random || encoding != "" {
		t.Fatalf("random bytes came back as %q (%q), %v; want them stored as is", same, encoding, err)
	}
}
