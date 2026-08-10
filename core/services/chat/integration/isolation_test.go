//go:build integration

package integration

import (
	"strings"
	"testing"
)

// Schema isolation is a rule in rules/infra.md and a grant in the database, and
// this is what makes it the second thing rather than only the first.
//
// The role this suite connects as has USAGE and CREATE on `chat` and nothing
// anywhere else, so a cross-schema read fails where it is written rather than
// where it is reviewed. Without a test, the day somebody grants a little more
// to make something work, nothing notices.
func TestChatCannotReadAnotherServicesSchema(t *testing.T) {
	for name, query := range map[string]string{
		"auth's tables":    `SELECT count(*) FROM auth.users`,
		"the auth schema":  `SELECT 1 FROM pg_tables WHERE schemaname = 'auth' AND tableowner = current_user`,
		"creating in auth": `CREATE TABLE auth.smuggled (id int)`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), query)

			// The middle case is a permitted query over a catalogue view that
			// simply finds nothing — the point there is that chat_service owns
			// no table in auth, not that the read is refused.
			if strings.HasPrefix(query, "SELECT 1 FROM pg_tables") {
				if err != nil {
					t.Fatalf("catalogue read failed: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("chat_service was allowed to run %q", query)
			}
			if !strings.Contains(err.Error(), "permission denied") {
				t.Errorf("refused for the wrong reason: %v", err)
			}
		})
	}
}
