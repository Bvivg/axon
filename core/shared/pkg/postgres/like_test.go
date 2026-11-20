package postgres

import "testing"

func TestContainingMatchesWildcardsLiterally(t *testing.T) {
	for in, want := range map[string]string{
		"hello":      "%hello%",
		"50%":        `%50\%%`,
		"snake_case": `%snake\_case%`,
		`C:\temp`:    `%C:\\temp%`,
		`\%_`:        `%\\\%\_%`,
	} {
		if got := Containing(in); got != want {
			t.Errorf("Containing(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStartingWithMatchesWildcardsLiterally(t *testing.T) {
	for in, want := range map[string]string{
		"ada":   "ada%",
		"a_b%c": `a\_b\%c%`,
	} {
		if got := StartingWith(in); got != want {
			t.Errorf("StartingWith(%q) = %q, want %q", in, got, want)
		}
	}
}
