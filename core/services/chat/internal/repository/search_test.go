package repository

import "testing"

func TestContainingMatchesWildcardsLiterally(t *testing.T) {
	for in, want := range map[string]string{
		"hello":      "%hello%",
		"50%":        `%50\%%`,
		"snake_case": `%snake\_case%`,
		`C:\temp`:    `%C:\\temp%`,
		`\%_`:        `%\\\%\_%`,
	} {
		if got := containing(in); got != want {
			t.Errorf("containing(%q) = %q, want %q", in, got, want)
		}
	}
}
