//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestChatCannotReadAnotherServicesSchema(t *testing.T) {
	for name, query := range map[string]string{
		"auth's tables":    `SELECT count(*) FROM auth.users`,
		"the auth schema":  `SELECT 1 FROM pg_tables WHERE schemaname = 'auth' AND tableowner = current_user`,
		"creating in auth": `CREATE TABLE auth.smuggled (id int)`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), query)

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
