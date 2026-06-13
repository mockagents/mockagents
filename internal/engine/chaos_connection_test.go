package engine

import (
	"context"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func connFaultMode(t *testing.T, err error) string {
	t.Helper()
	ce := AsChaosError(err)
	if ce == nil {
		t.Fatalf("expected a *ChaosError, got %v", err)
	}
	return ce.Connection
}

func TestChaosConnection_ByRate(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Connection: &types.ChaosConnectionConfig{Mode: "reset", Rate: 1},
	})
	err := inj.Before(context.Background(), agent)
	if got := connFaultMode(t, err); got != "reset" {
		t.Errorf("connection mode = %q, want reset", got)
	}
}

func TestChaosConnection_RateZeroNeverFires(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Connection: &types.ChaosConnectionConfig{Mode: "empty", Rate: 0},
	})
	// Rate 0 / no FailFirst → the section is inert (Before short-circuits).
	for i := 0; i < 20; i++ {
		if err := inj.Before(context.Background(), agent); err != nil {
			t.Fatalf("rate 0 should never fire, got %v", err)
		}
	}
}

func TestChaosConnection_FailFirst(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Connection: &types.ChaosConnectionConfig{Mode: "random", FailFirst: 2},
	})
	// First two requests fault, third recovers.
	for i := 0; i < 2; i++ {
		if got := connFaultMode(t, inj.Before(context.Background(), agent)); got != "random" {
			t.Fatalf("request %d: mode = %q, want random", i, got)
		}
	}
	if err := inj.Before(context.Background(), agent); err != nil {
		t.Errorf("3rd request should have recovered, got %v", err)
	}
}

func TestChaosConnection_FailFirstIndependentOfErrors(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	// Both error.fail_first and connection.fail_first are set; their counters
	// must be independent (errors evaluated first, then connection).
	agent := agentWithChaos(&types.ChaosConfig{
		Errors:     &types.ChaosErrorConfig{FailFirst: 1, StatusCode: 503},
		Connection: &types.ChaosConnectionConfig{Mode: "reset", FailFirst: 1},
	})
	// Request 1: the error fault fires first (FailFirst=1), short-circuiting
	// Before — the connection counter is NOT consumed.
	if ce := AsChaosError(inj.Before(context.Background(), agent)); ce == nil || ce.StatusCode != 503 {
		t.Fatalf("request 1 should be the 503 error fault, got %v", ce)
	}
	// Request 2: the error fault has recovered → the connection fault fires now
	// (its own counter still at its first fault).
	if got := connFaultMode(t, inj.Before(context.Background(), agent)); got != "reset" {
		t.Fatalf("request 2 should be the connection fault, got %q", got)
	}
	// Request 3: both recovered.
	if err := inj.Before(context.Background(), agent); err != nil {
		t.Errorf("request 3 should have recovered, got %v", err)
	}
}

func TestChaosConnection_ErrorString(t *testing.T) {
	ce := &ChaosError{Connection: "reset"}
	if ce.Error() != `chaos: connection fault "reset"` {
		t.Errorf("Error() = %q", ce.Error())
	}
}

func TestChaosConnection_InactiveWhenNil(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{}) // no sections
	if err := inj.Before(context.Background(), agent); err != nil {
		t.Errorf("empty chaos should be inert, got %v", err)
	}
}
