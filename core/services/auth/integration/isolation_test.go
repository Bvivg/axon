//go:build integration

package integration

import (
	"strings"
	"testing"
)

// Schema isolation is stated as a rule in rules/infra.md: one database, one
// schema per service, and no service reaching into another's. A rule that only
// exists in a document is a rule that gets broken by the first person who has
// not read it.
//
// These tests run as auth_service — the role the deployed service uses — and
// assert that the database refuses, rather than that nobody has tried yet.
func TestAuthRoleCannotReachAnotherServiceSchema(t *testing.T) {
	statements := map[string]string{
		"create a table":   `CREATE TABLE chat.smuggled (id int)`,
		"read":             `SELECT 1 FROM chat.anything`,
		"create a schema":  `CREATE SCHEMA sneaky`,
		"write to public":  `CREATE TABLE public.parked_here_for_now (id int)`,
		"change own grant": `GRANT USAGE ON SCHEMA chat TO auth_service`,
	}

	for name, statement := range statements {
		t.Run(name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), statement)
			if err == nil {
				t.Fatalf("the auth role was allowed to %s", name)
			}
			if !isPermissionDenied(err) {
				t.Fatalf("refused, but not by the permission model: %v", err)
			}
		})
	}
}

// The role's search_path is pinned to its own schema, so an unqualified name
// resolves there and nowhere else. Without it a query that forgets to qualify a
// table could silently find one somewhere along the path.
func TestUnqualifiedNamesResolveInTheAuthSchema(t *testing.T) {
	var schema string
	if err := pool.QueryRow(t.Context(),
		`SELECT schemaname FROM pg_tables WHERE tablename = 'users'`).Scan(&schema); err != nil {
		t.Fatalf("look up the users table: %v", err)
	}

	if schema != "auth" {
		t.Errorf("users resolves to schema %q, want auth", schema)
	}
}

// isPermissionDenied reports whether the database refused on authorisation
// grounds. Matching the text rather than only the SQLSTATE catches the
// "schema does not exist" that Postgres returns when a role cannot even see a
// schema — a refusal by another name.
func isPermissionDenied(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "must be owner")
}
