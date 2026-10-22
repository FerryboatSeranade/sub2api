package migrations

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestCompatibilityUpgradePreservesExistingPreferences(t *testing.T) {
	for _, name := range []string{"242_prune_openai_request_timezones.sql", "244_disable_codex_ticket_harvesting.sql"} {
		data, err := FS.ReadFile(name)
		require.NoError(t, err)
		sql := strings.ToUpper(string(data))
		require.NotContains(t, sql, "UPDATE ACCOUNTS")
		require.NotContains(t, sql, "INSERT INTO SETTINGS")
		require.Contains(t, sql, "SELECT 1;")
	}
}
