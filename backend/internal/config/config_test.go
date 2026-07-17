package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/config"
)

func writeDotenv(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// unsetenv clears a variable for the test's duration, restoring it after.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	if prior, ok := os.LookupEnv(key); ok {
		t.Cleanup(func() { require.NoError(t, os.Setenv(key, prior)) })
		require.NoError(t, os.Unsetenv(key))
	}
}

func TestLoadDefaults(t *testing.T) {
	unsetenv(t, "PORTAL_HTTP_ADDR")
	unsetenv(t, "PORTAL_DB_NAME")
	unsetenv(t, "PORTAL_AUTH_MODE")
	unsetenv(t, "PORTAL_BREAKGLASS_HASH")
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.HTTPAddr)
	require.Equal(t, "portal", cfg.DBName)
	// SPEC-020 mini-ADR 4: an unset auth mode MUST land on ldap — the mode
	// that fails closed. "off" as a default would be an open door.
	require.Equal(t, "ldap", cfg.AuthMode)
	require.Empty(t, cfg.BreakglassHash, "break-glass must be disabled by default")
}

func TestLoadMissingDotenvIsNotAnError(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "no-such-file"))
	require.NoError(t, err)
}

func TestLoadPrecedence(t *testing.T) {
	unsetenv(t, "PORTAL_HTTP_ADDR")
	unsetenv(t, "PORTAL_DB_HOST")
	unsetenv(t, "PORTAL_DB_PORT")
	dotenv := writeDotenv(t, "PORTAL_HTTP_ADDR=:9999\nPORTAL_DB_HOST=filehost\n")

	// dotenv beats defaults
	cfg, err := config.Load(dotenv)
	require.NoError(t, err)
	require.Equal(t, ":9999", cfg.HTTPAddr)
	require.Equal(t, "filehost", cfg.DBHost)
	require.Equal(t, 5432, cfg.DBPort) // untouched default survives

	// process env beats dotenv
	t.Setenv("PORTAL_HTTP_ADDR", ":7777")
	cfg, err = config.Load(dotenv)
	require.NoError(t, err)
	require.Equal(t, ":7777", cfg.HTTPAddr)
	require.Equal(t, "filehost", cfg.DBHost) // other file values still apply
}

func TestLoadDoesNotMutateProcessEnv(t *testing.T) {
	unsetenv(t, "PORTAL_DB_HOST")
	dotenv := writeDotenv(t, "PORTAL_DB_HOST=filehost\n")
	_, err := config.Load(dotenv)
	require.NoError(t, err)
	_, present := os.LookupEnv("PORTAL_DB_HOST")
	require.False(t, present, "Load must not leak dotenv values into the process env")
}

func TestDSN(t *testing.T) {
	cfg := config.Config{
		DBHost: "127.0.0.1", DBPort: 5432,
		DBUser: "portal", DBPassword: "p@ss/word", DBName: "portal",
	}
	require.Equal(t, "postgres://portal:p%40ss%2Fword@127.0.0.1:5432/portal", cfg.DSN())
}

func TestSemaphoreTemplateMap(t *testing.T) {
	m, err := config.Config{SemaphoreTemplates: "smoke:1, dump:3"}.SemaphoreTemplateMap()
	require.NoError(t, err)
	require.Equal(t, map[string]int{"smoke": 1, "dump": 3}, m)

	empty, err := config.Config{}.SemaphoreTemplateMap()
	require.NoError(t, err)
	require.Empty(t, empty, "empty value = empty map; fail-closed is per-StartJob on an unmapped tag")

	// A typo must fail at boot (SPEC-033 mini-ADR 3), never silently drop a mapping.
	for _, bad := range []string{"smoke", "smoke:", ":1", "smoke:notanint"} {
		_, err := config.Config{SemaphoreTemplates: bad}.SemaphoreTemplateMap()
		require.Error(t, err, "malformed entry %q must error", bad)
	}
}

func TestProtectedInstanceSet(t *testing.T) {
	// Seeded with the portal's own DB name (SPEC-042 mini-ADR 5), unioned with
	// the explicit list, all lowercased for case-insensitive match.
	set := config.Config{DBName: "portal", ProtectedInstances: "Ops-DB, secrets-store"}.
		ProtectedInstanceSet()
	require.Equal(t, map[string]bool{
		"portal": true, "ops-db": true, "secrets-store": true,
	}, set)

	// Empty list still protects the portal DB out of the box.
	require.Equal(t, map[string]bool{"portal": true},
		config.Config{DBName: "portal"}.ProtectedInstanceSet())
}

func TestEngineNonProdDefaultsToMock(t *testing.T) {
	unsetenv(t, "PORTAL_ENGINE_NONPROD")
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, "mock", cfg.EngineNonProd, "the forever-default engine (ADR-002); semaphore is opt-in")
}
