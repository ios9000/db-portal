package schedule

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
)

// FireDue exposes one executor pass to tests — deterministic, no ticker.
func (s *Service) FireDue(ctx context.Context) { s.fireDue(ctx) }

// NextFire exposes the fire-time computation so jitter bounds are testable
// as the pure function they are.
func (s *Service) NextFire(spec cron.Schedule, from time.Time) time.Time {
	return s.nextFire(spec, from)
}
