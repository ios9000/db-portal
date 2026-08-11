package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	cfg, _, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.HTTPAddr)
	require.Equal(t, "portal", cfg.DBName)
	// SPEC-020 mini-ADR 4: an unset auth mode MUST land on ldap — the mode
	// that fails closed. "off" as a default would be an open door.
	require.Equal(t, "ldap", cfg.AuthMode)
	require.Empty(t, cfg.BreakglassHash, "break-glass must be disabled by default")
}

func TestLoadMissingDotenvIsNotAnError(t *testing.T) {
	_, _, err := config.Load(filepath.Join(t.TempDir(), "no-such-file"))
	require.NoError(t, err)
}

// WU-049 (m4-gate finding 4): Load reports whether a dotenv file was ACTUALLY
// read, so main logs "loaded dotenv file" only when one was — an explicit
// PORTAL_DOTENV pointing at a missing path must not masquerade as loaded
// while every value silently fell back to process env + defaults.
func TestLoadReportsWhetherDotenvWasRead(t *testing.T) {
	_, loaded, err := config.Load(writeDotenv(t, "PORTAL_DB_HOST=filehost\n"))
	require.NoError(t, err)
	require.True(t, loaded, "a real file was read")

	_, loaded, err = config.Load(filepath.Join(t.TempDir(), "typo.env"))
	require.NoError(t, err)
	require.False(t, loaded, "a missing explicit path reads nothing")

	_, loaded, err = config.Load("")
	require.NoError(t, err)
	require.False(t, loaded, "no path, no file")
}

// WU-049 (m4-gate findings 2+3): the deploy env template must stay parseable
// by systemd's EnvironmentFile=, which — unlike dev's godotenv — does NOT
// strip a trailing `# comment` after a value: the comment becomes part of the
// value, the typed config fields fail ParseBool/ParseDuration, and the pilot
// unit crash-loops. Guard both legs: (a) no `#` (and no stray whitespace/
// quoting) in any value; (b) the template's values, taken VERBATIM the way
// EnvironmentFile= delivers them, round-trip through config.Load and pass
// config.Validate — so a regression fails here, not on a fresh pilot host.
func TestDeployEnvTemplateSystemdSafe(t *testing.T) {
	raw, err := os.ReadFile("../../../infra/portal.env.template")
	require.NoError(t, err)

	keyRe := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	for n, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue // blank or whole-line comment — fine
		}
		key, value, ok := strings.Cut(line, "=")
		require.True(t, ok, "line %d: not KEY=VALUE nor a comment: %q", n+1, line)
		require.Regexp(t, keyRe, key, "line %d: bad key (no `export`, no leading space)", n+1)
		require.NotContains(t, value, "#",
			"line %d: EnvironmentFile= does not strip trailing comments — put the note on its own line", n+1)
		require.Equal(t, strings.TrimSpace(value), value,
			"line %d: stray whitespace around the value", n+1)
		require.NotContains(t, value, `"`, "line %d: unquoted plain values only", n+1)

		t.Setenv(key, value) // verbatim, exactly as EnvironmentFile= would
	}

	cfg, _, err := config.Load("")
	require.NoError(t, err, "template values must parse (bool/duration/int fields)")
	require.NoError(t, cfg.Validate(), "the template's ldap mode must ship its required vars")
	require.Equal(t, "ldap", cfg.AuthMode, "a pilot template must not ship auth off")
	require.True(t, cfg.CookieSecure, "a pilot template ships Secure cookies")
}

func TestLoadPrecedence(t *testing.T) {
	unsetenv(t, "PORTAL_HTTP_ADDR")
	unsetenv(t, "PORTAL_DB_HOST")
	unsetenv(t, "PORTAL_DB_PORT")
	dotenv := writeDotenv(t, "PORTAL_HTTP_ADDR=:9999\nPORTAL_DB_HOST=filehost\n")

	// dotenv beats defaults
	cfg, _, err := config.Load(dotenv)
	require.NoError(t, err)
	require.Equal(t, ":9999", cfg.HTTPAddr)
	require.Equal(t, "filehost", cfg.DBHost)
	require.Equal(t, 5432, cfg.DBPort) // untouched default survives

	// process env beats dotenv
	t.Setenv("PORTAL_HTTP_ADDR", ":7777")
	cfg, _, err = config.Load(dotenv)
	require.NoError(t, err)
	require.Equal(t, ":7777", cfg.HTTPAddr)
	require.Equal(t, "filehost", cfg.DBHost) // other file values still apply
}

func TestLoadDoesNotMutateProcessEnv(t *testing.T) {
	unsetenv(t, "PORTAL_DB_HOST")
	dotenv := writeDotenv(t, "PORTAL_DB_HOST=filehost\n")
	_, _, err := config.Load(dotenv)
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

func TestLocateDotenvExplicitOverride(t *testing.T) {
	// PORTAL_DOTENV wins verbatim — an explicit path, or "" to disable the
	// search (the prod/systemd escape hatch).
	t.Setenv("PORTAL_DOTENV", "/etc/dbportal/portal.env")
	require.Equal(t, "/etc/dbportal/portal.env", config.LocateDotenv())

	t.Setenv("PORTAL_DOTENV", "")
	require.Empty(t, config.LocateDotenv(), "empty PORTAL_DOTENV disables the .env search")
}

func TestLocateDotenvFindsRepoRootEnv(t *testing.T) {
	// Dev runs from a subdir (e.g. backend/internal/config) while .env lives at
	// the repo root; the bounded walk still finds it.
	unsetenv(t, "PORTAL_DOTENV")
	repo := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(repo, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("PORTAL_DB_NAME=fromrepo\n"), 0o600))
	sub := filepath.Join(repo, "backend", "internal", "config")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	got := config.LocateDotenv()
	require.NotEmpty(t, got, "the repo-root .env must be found from a subdir")
	b, err := os.ReadFile(got)
	require.NoError(t, err)
	require.Contains(t, string(b), "fromrepo")
}

func TestLocateDotenvStopsAtRepoRoot(t *testing.T) {
	// The footgun (M1-gate item 16): a .env in a parent ABOVE the repo root
	// must never be adopted. Layout: outer/.env (foreign) → outer/repo/.git
	// (the repo root, no .env of its own) → outer/repo/backend (the CWD).
	unsetenv(t, "PORTAL_DOTENV")
	outer := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outer, ".env"), []byte("PORTAL_DB_NAME=foreign\n"), 0o600))
	repo := filepath.Join(outer, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	sub := filepath.Join(repo, "backend")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	require.Empty(t, config.LocateDotenv(),
		"must not walk above the repo root into a foreign parent .env")
}

func TestLocateDotenvOutsideRepoChecksCwdOnly(t *testing.T) {
	// No repo marker anywhere: the boundary is the CWD itself, so a parent .env
	// is still never adopted, but a .env in the CWD is.
	unsetenv(t, "PORTAL_DOTENV")
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	t.Chdir(sub)
	if config.LocateDotenv() != "" {
		t.Skip("temp dir sits under an unexpected .git/.env ancestor; skipping the rootless case")
	}

	require.NoError(t, os.WriteFile(filepath.Join(base, ".env"), []byte("PORTAL_DB_NAME=parent\n"), 0o600))
	require.Empty(t, config.LocateDotenv(), "outside a repo, a parent .env must be ignored")

	require.NoError(t, os.WriteFile(filepath.Join(sub, ".env"), []byte("PORTAL_DB_NAME=here\n"), 0o600))
	got := config.LocateDotenv()
	require.NotEmpty(t, got)
	b, err := os.ReadFile(got)
	require.NoError(t, err)
	require.Contains(t, string(b), "here")
}

func TestValidate(t *testing.T) {
	// ldap mode without its bind vars boots into a door nobody can open —
	// Validate refuses instead, naming every missing var (WU-046).
	err := config.Config{AuthMode: "ldap"}.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "PORTAL_LDAP_URL")
	require.Contains(t, err.Error(), "PORTAL_LDAP_BIND_TEMPLATE")

	// Partially configured still fails, naming only what's missing.
	err = config.Config{AuthMode: "ldap", LDAPURL: "ldaps://ad.corp:636"}.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "PORTAL_LDAP_BIND_TEMPLATE")
	require.NotContains(t, err.Error(), "PORTAL_LDAP_URL")

	// Fully configured ldap validates.
	require.NoError(t, config.Config{
		AuthMode: "ldap", LDAPURL: "ldaps://ad.corp:636", LDAPBindTemplate: "%s@corp.example.com",
	}.Validate())

	// fake / off never require the ldap vars.
	require.NoError(t, config.Config{AuthMode: "fake"}.Validate())
	require.NoError(t, config.Config{AuthMode: "off"}.Validate())
}

func TestEngineNonProdDefaultsToMock(t *testing.T) {
	unsetenv(t, "PORTAL_ENGINE_NONPROD")
	cfg, _, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, "mock", cfg.EngineNonProd, "the forever-default engine (ADR-002); semaphore is opt-in")
}
