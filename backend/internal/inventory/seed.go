package inventory

import (
	"encoding/csv"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
)

// SPEC-041: a deterministic generator for a realistic ~500-instance estate.
// It emits a valid SPEC-010 CSV that GenerateEstate's callers feed straight
// through Import, so idempotency, cluster resolution, and validation all come
// from the existing path — the generator only has to produce clean rows.

// seedApps is the pool of application (cluster) base names. When more clusters
// are needed than the pool holds, a numeric group suffix keeps names distinct
// and still DNS-label-valid (e.g. billing, then billing-2).
var seedApps = []string{
	"billing", "shop", "crm", "wms", "analytics", "hr", "payments", "inventory",
	"logistics", "catalog", "search", "notifications", "auth", "reporting",
	"ledger", "orders", "pricing", "fulfillment", "returns", "loyalty",
	"tax", "gateway", "scheduler", "identity",
}

var seedPGVersions = []string{"15.7", "16.2", "16.3", "16.4", "17.1"}

// Prod always carries a window; test sometimes; dev never — exercising the
// WU-023 window parser at fleet scale.
var (
	seedProdWindows = []string{"Sat 02:00-06:00", "Sun 01:00-05:00", "Sat 22:00-02:00"}
	seedTestWindows = []string{"Fri 20:00-23:00", "Wed 01:00-03:00", "Thu 18:00-22:00"}
)

type seedCluster struct {
	name     string
	platform string
}

// GenerateEstate returns a SPEC-010 CSV of n instance rows for the given seed.
// It is a pure function of (n, seed): the same arguments yield byte-identical
// output, and every row parses clean (SPEC-041 behaviors 1-2). n <= 0 yields a
// header-only CSV.
func GenerateEstate(n int, seed int64) string {
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)))

	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write(header)
	if n <= 0 {
		w.Flush()
		return b.String()
	}

	// Env mix is computed up front and shuffled, so the ratios hold exactly
	// (prod a non-empty minority, non-prod dominant) rather than drifting with
	// the RNG (SPEC-041 behavior 4).
	nProd := n * 18 / 100
	if nProd < 1 {
		nProd = 1
	}
	nTest := n * 32 / 100
	nDev := n - nProd - nTest
	if nDev < 0 { // tiny n: never let dev go negative
		nDev = 0
		nTest = n - nProd
	}
	envs := make([]string, 0, n)
	for range nProd {
		envs = append(envs, "prod")
	}
	for range nTest {
		envs = append(envs, "test")
	}
	for range nDev {
		envs = append(envs, "dev")
	}
	rng.Shuffle(len(envs), func(i, j int) { envs[i], envs[j] = envs[j], envs[i] })

	numClusters := n / 8
	if numClusters < 5 {
		numClusters = 5
	}
	clusters := make([]seedCluster, numClusters)
	for c := range clusters {
		clusters[c] = seedCluster{name: seedClusterName(c), platform: seedPlatform(c)}
	}

	// Per-(cluster, env) running counter gives each instance a unique,
	// readable name: <cluster>-<env>-<NN>.
	counters := make([]map[string]int, numClusters)
	for c := range counters {
		counters[c] = map[string]int{}
	}

	for i := range n {
		c := i % numClusters
		env := envs[i]
		cl := clusters[c]

		counters[c][env]++
		name := fmt.Sprintf("%s-%s-%02d", cl.name, env, counters[c][env])

		// Fixed RNG-call order per row keeps output deterministic.
		pgVersion := seedPGVersions[rng.IntN(len(seedPGVersions))]
		size := seedSize(env, rng)
		window := seedWindow(env, rng)

		_ = w.Write([]string{
			name, cl.name, env, cl.platform, pgVersion, size, cl.name + "-team", window,
		})
	}
	w.Flush()
	return b.String()
}

// seedClusterName maps a cluster index to a distinct DNS-label-valid name,
// suffixing with a group number once the base pool is exhausted.
func seedClusterName(c int) string {
	base := seedApps[c%len(seedApps)]
	if group := c / len(seedApps); group > 0 {
		return base + "-" + strconv.Itoa(group+1)
	}
	return base
}

// seedPlatform assigns each cluster ONE platform (a cluster with two platforms
// would self-quarantine, csv.go behavior 8) — a deterministic mix.
func seedPlatform(c int) string {
	if c%2 == 0 {
		return "k8s_patroni"
	}
	return "vm"
}

// seedSize gives prod instances realistically larger volumes than non-prod.
func seedSize(env string, rng *rand.Rand) string {
	switch env {
	case "prod":
		return strconv.Itoa(100 + rng.IntN(900))
	case "test":
		return strconv.Itoa(5 + rng.IntN(80))
	default: // dev
		return strconv.Itoa(1 + rng.IntN(40))
	}
}

func seedWindow(env string, rng *rand.Rand) string {
	switch env {
	case "prod":
		return seedProdWindows[rng.IntN(len(seedProdWindows))]
	case "test":
		if rng.IntN(10) < 3 { // ~30% of test instances carry a window
			return seedTestWindows[rng.IntN(len(seedTestWindows))]
		}
		return ""
	default: // dev
		return ""
	}
}
