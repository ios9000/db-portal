package inventory_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/inventory"
)

const goodHeader = "instance_name,cluster_name,env,platform,pg_version,size_gb,owner,maintenance_window"

func TestParseFileLevelFailures(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty file", ""},
		{"data row instead of header", "billing-prod,billing,prod,k8s_patroni,16.3,412,team,\n"},
		{"reordered header", "cluster_name,instance_name,env,platform,pg_version,size_gb,owner,maintenance_window\n"},
		{"unknown column", goodHeader + ",extra\n"},
		{"missing column", strings.TrimSuffix(goodHeader, ",maintenance_window") + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := inventory.Parse(strings.NewReader(tc.input))
			require.Error(t, err)
		})
	}
}

func TestParseHeaderOnlyIsValid(t *testing.T) {
	res, err := inventory.Parse(strings.NewReader(goodHeader + "\n"))
	require.NoError(t, err)
	require.Zero(t, res.Total)
	require.Empty(t, res.Rows)
	require.Empty(t, res.Rejects)
}

// TestParseRowValidation covers SPEC-010 behaviors 4 and 7: each bad row is
// quarantined with ALL its reasons; valid rows still parse.
func TestParseRowValidation(t *testing.T) {
	tests := []struct {
		name        string
		row         string
		wantReasons []string // substring per expected reason; empty = accepted
	}{
		{"valid full row", "billing-prod,billing,prod,k8s_patroni,16.3,412,billing-team,Sat 02:00-06:00", nil},
		{"valid empty optionals", "hr-test,hr,test,vm,16.4,,hr-team,", nil},
		{"unknown env", "billing-prod,billing,prod2,k8s_patroni,16.3,412,team,", []string{"unknown env"}},
		{"blank instance_name", ",billing,prod,k8s_patroni,16.3,412,team,", []string{"instance_name is required"}},
		{"uppercase instance_name", "Billing-Prod,billing,prod,k8s_patroni,16.3,412,team,", []string{"instance_name must match"}},
		{"leading dash cluster_name", "billing-prod,-billing,prod,k8s_patroni,16.3,412,team,", []string{"cluster_name must match"}},
		{"overlong instance_name", strings.Repeat("a", 64) + ",billing,prod,k8s_patroni,16.3,412,team,", []string{"instance_name must match"}},
		{"non-numeric size_gb", "billing-prod,billing,prod,k8s_patroni,16.3,huge,team,", []string{"size_gb must be numeric"}},
		{"infinite size_gb", "billing-prod,billing,prod,k8s_patroni,16.3,inf,team,", []string{"size_gb must be numeric"}},
		{"truncated hex size_gb", "billing-prod,billing,prod,k8s_patroni,16.3,0x1p,team,", []string{"size_gb must be numeric"}},
		{"too few columns", "billing-prod,billing,prod,k8s_patroni", []string{"wrong column count: got 4, want 8"}},
		{"too many columns", "billing-prod,billing,prod,k8s_patroni,16.3,412,team,,extra", []string{"wrong column count: got 9, want 8"}},
		{"unknown platform", "billing-prod,billing,prod,docker,16.3,412,team,", []string{"unknown platform"}},
		{"blank pg_version", "billing-prod,billing,prod,k8s_patroni,,412,team,", []string{"pg_version is required"}},
		{"blank owner", "billing-prod,billing,prod,k8s_patroni,16.3,412,,", []string{"owner is required"}},
		{
			"reasons accumulate",
			",billing,prod2,k8s_patroni,,nan,team,",
			[]string{"instance_name is required", "unknown env", "pg_version is required", "size_gb must be numeric"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := inventory.Parse(strings.NewReader(goodHeader + "\n" + tc.row + "\n"))
			require.NoError(t, err)
			require.Equal(t, 1, res.Total)

			if len(tc.wantReasons) == 0 {
				require.Len(t, res.Rows, 1)
				require.Empty(t, res.Rejects)
				return
			}
			require.Empty(t, res.Rows)
			require.Len(t, res.Rejects, 1)
			rej := res.Rejects[0]
			require.Equal(t, 2, rej.Line)
			require.Equal(t, tc.row, rej.Raw, "raw line must be stored verbatim")
			require.Len(t, rej.Reasons, len(tc.wantReasons), "reasons: %v", rej.Reasons)
			for _, want := range tc.wantReasons {
				require.Condition(t, func() bool {
					for _, got := range rej.Reasons {
						if strings.Contains(got, want) {
							return true
						}
					}
					return false
				}, "missing reason %q in %v", want, rej.Reasons)
			}
		})
	}
}

// WU-018 (m1-gate item 8): size_gb is kept in one canonical decimal form, so
// exotic forms Go accepts must either canonicalize or quarantine — the raw
// text never reaches Postgres, which would reject hex floats (aborting the
// import) or normalize '1e2'-style forms (defeating the unchanged compare).
func TestParseSizeGBCanonicalized(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"412", "412"},
		{"16.3", "16.3"},
		{"1e2", "100"},
		{".5", "0.5"},
		{"0120", "120"},
		{"0x1p4", "16"},
		{"+7", "7"},
		{"-0", "0"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			row := "billing-prod,billing,prod,k8s_patroni,16.3," + tc.in + ",team,"
			res, err := inventory.Parse(strings.NewReader(goodHeader + "\n" + row + "\n"))
			require.NoError(t, err)
			require.Len(t, res.Rows, 1, "rejects: %v", res.Rejects)
			require.NotNil(t, res.Rows[0].SizeGB)
			require.Equal(t, tc.want, *res.Rows[0].SizeGB)
		})
	}
}

// SPEC-010 behavior 5: first row wins, later duplicates quarantined.
func TestParseDuplicateInstanceName(t *testing.T) {
	input := goodHeader + "\n" +
		"billing-prod,billing,prod,k8s_patroni,16.3,412,team,\n" +
		"billing-prod,billing,prod,k8s_patroni,17.0,412,team,\n"
	res, err := inventory.Parse(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, res.Rows, 1)
	require.Equal(t, "16.3", res.Rows[0].PGVersion, "first row wins")
	require.Len(t, res.Rejects, 1)
	require.Equal(t, []string{"duplicate instance_name in file"}, res.Rejects[0].Reasons)
}

// A row quarantined for its own problems must not reserve its instance name.
func TestParseInvalidRowDoesNotReserveName(t *testing.T) {
	input := goodHeader + "\n" +
		"billing-prod,billing,prod2,k8s_patroni,16.3,412,team,\n" +
		"billing-prod,billing,prod,k8s_patroni,16.3,412,team,\n"
	res, err := inventory.Parse(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, res.Rows, 1)
	require.Len(t, res.Rejects, 1)
	require.Contains(t, res.Rejects[0].Reasons[0], "unknown env")
}

// SPEC-010 behavior 8: same cluster, different platform — later row loses.
func TestParseClusterPlatformConflict(t *testing.T) {
	input := goodHeader + "\n" +
		"billing-prod,billing,prod,k8s_patroni,16.3,412,team,\n" +
		"billing-test,billing,test,vm,16.3,38,team,\n"
	res, err := inventory.Parse(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, res.Rows, 1)
	require.Len(t, res.Rejects, 1)
	require.Contains(t, res.Rejects[0].Reasons[0], "cluster platform conflict")
}

// The shipped fixture must parse clean: 8 instances across 6 clusters.
func TestParseFixture(t *testing.T) {
	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	res, err := inventory.Parse(f)
	require.NoError(t, err)
	require.Empty(t, res.Rejects)
	require.Len(t, res.Rows, 8)

	clusters := map[string]bool{}
	for _, r := range res.Rows {
		clusters[r.ClusterName] = true
	}
	require.Len(t, clusters, 6)
}

// SPEC-050 mini-ADR 5 (WU-051): the optional trailing host,port columns —
// per-file (header-declared), absent → nil, and the old 8-column format
// parses exactly as before.
func TestParseConnColumns(t *testing.T) {
	connHeader := goodHeader + ",host,port"

	t.Run("tuple round-trips", func(t *testing.T) {
		res, err := inventory.Parse(strings.NewReader(
			connHeader + "\nbilling-test,billing,test,vm,16.3,40,team,,10.0.0.5,5433\n"))
		require.NoError(t, err)
		require.Empty(t, res.Rejects)
		require.Len(t, res.Rows, 1)
		require.NotNil(t, res.Rows[0].Host)
		require.Equal(t, "10.0.0.5", *res.Rows[0].Host)
		require.NotNil(t, res.Rows[0].Port)
		require.Equal(t, 5433, *res.Rows[0].Port)
	})

	t.Run("empty tuple stays nil", func(t *testing.T) {
		res, err := inventory.Parse(strings.NewReader(
			connHeader + "\nbilling-test,billing,test,vm,16.3,40,team,,,\n"))
		require.NoError(t, err)
		require.Empty(t, res.Rejects)
		require.Nil(t, res.Rows[0].Host)
		require.Nil(t, res.Rows[0].Port)
	})

	t.Run("bare host defaults the port later", func(t *testing.T) {
		res, err := inventory.Parse(strings.NewReader(
			connHeader + "\nbilling-test,billing,test,vm,16.3,40,team,,db1.corp,\n"))
		require.NoError(t, err)
		require.Empty(t, res.Rejects)
		require.Equal(t, "db1.corp", *res.Rows[0].Host)
		require.Nil(t, res.Rows[0].Port)
	})

	t.Run("old format has no tuple fields", func(t *testing.T) {
		res, err := inventory.Parse(strings.NewReader(
			goodHeader + "\nbilling-test,billing,test,vm,16.3,40,team,\n"))
		require.NoError(t, err)
		require.Empty(t, res.Rejects)
		require.Nil(t, res.Rows[0].Host)
		require.Nil(t, res.Rows[0].Port)
	})

	t.Run("old-format row count unchanged by the new header", func(t *testing.T) {
		// A 10-field row under the 8-column header still quarantines.
		res, err := inventory.Parse(strings.NewReader(
			goodHeader + "\nbilling-test,billing,test,vm,16.3,40,team,,10.0.0.5,5433\n"))
		require.NoError(t, err)
		require.Len(t, res.Rejects, 1)
		require.Contains(t, res.Rejects[0].Reasons[0], "wrong column count: got 10, want 8")
	})

	quarantines := []struct {
		name, row, reason string
	}{
		{"non-integer port", "billing-test,billing,test,vm,16.3,40,team,,10.0.0.5,http", "port must be an integer in 1..65535"},
		{"zero port", "billing-test,billing,test,vm,16.3,40,team,,10.0.0.5,0", "port must be an integer in 1..65535"},
		{"overflow port", "billing-test,billing,test,vm,16.3,40,team,,10.0.0.5,70000", "port must be an integer in 1..65535"},
		{"port without host", "billing-test,billing,test,vm,16.3,40,team,,,5433", "port without host"},
		{"whitespace host", "billing-test,billing,test,vm,16.3,40,team,,\"bad host\",5433", "host must be a hostname or address"},
		{"missing conn columns", "billing-test,billing,test,vm,16.3,40,team,", "wrong column count: got 8, want 10"},
	}
	for _, tc := range quarantines {
		t.Run(tc.name, func(t *testing.T) {
			res, err := inventory.Parse(strings.NewReader(connHeader + "\n" + tc.row + "\n"))
			require.NoError(t, err)
			require.Empty(t, res.Rows)
			require.Len(t, res.Rejects, 1)
			found := false
			for _, got := range res.Rejects[0].Reasons {
				if strings.Contains(got, tc.reason) {
					found = true
				}
			}
			require.True(t, found, "missing reason %q in %v", tc.reason, res.Rejects[0].Reasons)
		})
	}
}
