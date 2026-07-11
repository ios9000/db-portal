package engine_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/config"
	"github.com/ios9000/db-portal/backend/internal/engine"
)

// TestSemaphoreIntegration drives a REAL Semaphore (the compose service) end
// to end through the adapter. It SKIPS cleanly when Semaphore is unconfigured
// or unreachable — so `npm run check` and CI (which have no Semaphore) stay
// green (ADR-002's one skip-gated integration test; the testutil.MigratedDB
// skip-without-compose pattern). On the VM, where the compose service is up
// and .env carries the bootstrap token, it runs.
func TestSemaphoreIntegration(t *testing.T) {
	cfg, err := config.Load(config.LocateDotenv())
	require.NoError(t, err)
	if cfg.SemaphoreAPIToken == "" {
		t.Skip("PORTAL_SEMAPHORE_API_TOKEN unset (run infra/semaphore-bootstrap.sh) — skipping live Semaphore itest")
	}
	templates, err := cfg.SemaphoreTemplateMap()
	require.NoError(t, err)
	if _, ok := templates["smoke"]; !ok {
		t.Skip("no 'smoke' template in PORTAL_SEMAPHORE_TEMPLATES — skipping")
	}
	if !semaphoreReachable(cfg.SemaphoreURL) {
		t.Skipf("Semaphore not reachable at %s (compose service down?) — skipping", cfg.SemaphoreURL)
	}

	a := engine.NewSemaphoreAdapter(engine.SemaphoreConfig{
		BaseURL:      cfg.SemaphoreURL,
		APIToken:     cfg.SemaphoreAPIToken,
		ProjectID:    cfg.SemaphoreProjectID,
		Templates:    templates,
		PollInterval: 500 * time.Millisecond,
	}, discardLog())
	ctx := context.Background()

	t.Run("smoke runs to success with streamed logs", func(t *testing.T) {
		id, err := a.StartJob(ctx, "smoke", nil)
		require.NoError(t, err)
		require.NotEmpty(t, id)

		ch, err := a.StreamLogs(ctx, id)
		require.NoError(t, err)
		lines := make(chan int, 1)
		go func() {
			n := 0
			for range ch {
				n++
			}
			lines <- n // the stream closed → the job is terminal
		}()

		var final engine.JobStatus
		require.Eventually(t, func() bool {
			final, err = a.Status(ctx, id)
			require.NoError(t, err)
			return final.State.Terminal()
		}, 45*time.Second, 500*time.Millisecond)
		require.Equal(t, engine.StateSuccess, final.State)

		select {
		case n := <-lines:
			require.Positive(t, n, "the log stream carried real ansible output, then closed")
		case <-time.After(5 * time.Second):
			t.Fatal("log stream did not close after the job finished")
		}
	})

	t.Run("cancel mid-run maps to canceled", func(t *testing.T) {
		id, err := a.StartJob(ctx, "smoke", nil)
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			st, err := a.Status(ctx, id)
			require.NoError(t, err)
			return st.State == engine.StateRunning
		}, 25*time.Second, 300*time.Millisecond, "the task must reach running before we cancel it")

		require.NoError(t, a.Cancel(ctx, id))
		var final engine.JobStatus
		require.Eventually(t, func() bool {
			final, err = a.Status(ctx, id)
			require.NoError(t, err)
			return final.State.Terminal()
		}, 25*time.Second, 300*time.Millisecond)
		require.Equal(t, engine.StateCanceled, final.State)
	})
}

func semaphoreReachable(baseURL string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(baseURL + "/api/ping")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
