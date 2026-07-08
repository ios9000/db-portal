package engine_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
)

func TestRegistryResolvesPerClass(t *testing.T) {
	// Two DISTINCT adapter instances — prod and nonprod never share
	// configuration (guardrail layer 3).
	prod := engine.NewMockEngine(engine.MockConfig{Name: "mock-prod", StepDelay: time.Millisecond})
	nonprod := engine.NewMockEngine(engine.MockConfig{Name: "mock-nonprod", StepDelay: time.Millisecond})

	r := engine.NewRegistry()
	r.Register(engine.ClassProd, prod)
	r.Register(engine.ClassNonProd, nonprod)

	gotProd, err := r.For(engine.ClassProd)
	require.NoError(t, err)
	gotNonProd, err := r.For(engine.ClassNonProd)
	require.NoError(t, err)
	require.Same(t, engine.Adapter(prod), gotProd)
	require.Same(t, engine.Adapter(nonprod), gotNonProd)
	require.NotSame(t, gotProd, gotNonProd)
}

// SPEC-015 behavior 8 (guardrail layer 3, structural): one adapter instance
// — one engine config/credential set — can never serve both env classes,
// mocks included. Wiring that tries must die before the portal serves.
func TestRegisterRejectsSharedAdapter(t *testing.T) {
	shared := engine.NewMockEngine(engine.MockConfig{Name: "mock-shared", StepDelay: time.Millisecond})
	r := engine.NewRegistry()
	r.Register(engine.ClassProd, shared)

	require.Panics(t, func() { r.Register(engine.ClassNonProd, shared) },
		"same adapter instance for a second env class must panic")
	// Same class again is idempotent wiring, not sharing.
	require.NotPanics(t, func() { r.Register(engine.ClassProd, shared) })
}

func TestRegistryFailsClosedWhenUnregistered(t *testing.T) {
	r := engine.NewRegistry()
	_, err := r.For(engine.ClassProd)
	require.ErrorIs(t, err, engine.ErrNoAdapter)
}

func TestClassForEnv(t *testing.T) {
	cases := []struct {
		env  string
		want engine.EnvClass
	}{
		{"prod", engine.ClassProd},
		{"test", engine.ClassNonProd},
		{"dev", engine.ClassNonProd},
	}
	for _, c := range cases {
		got, err := engine.ClassForEnv(c.env)
		require.NoError(t, err)
		require.Equal(t, c.want, got)
	}
}

func TestClassForEnvFailsClosedOnUnknown(t *testing.T) {
	_, err := engine.ClassForEnv("staging")
	require.ErrorIs(t, err, engine.ErrUnknownEnv)
}
