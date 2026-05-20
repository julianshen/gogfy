package llm

import (
	"context"
	"testing"
)

func TestApproxInputTokensConservative(t *testing.T) {
	// The ~3.5-chars-per-token heuristic should round UP for budget
	// purposes — we'd rather over-estimate input tokens than
	// under-estimate. The +2/3 form produces ceil-ish behavior.
	cases := map[int]int{
		0:    0,
		1:    1,
		3:    1,
		10:   4,    // 10/3 = 3.33 → 4
		100:  34,   // 100/3 = 33.33 → 34
		3500: 1167, // ballpark "1 page → ~1k tokens"
	}
	for bytes, want := range cases {
		if got := ApproxInputTokens(bytes); got != want {
			t.Errorf("ApproxInputTokens(%d) = %d, want %d", bytes, got, want)
		}
	}
}

func TestApproxOutputTokensCapsAtMax(t *testing.T) {
	// Semantic extraction's output is bounded by the max_tokens
	// request parameter. A huge input shouldn't predict an
	// unbounded output budget; cap at maxTokens.
	if got := ApproxOutputTokens(100_000, 4096); got != 4096 {
		t.Errorf("expected output capped at 4096, got %d", got)
	}
	if got := ApproxOutputTokens(100_000, 0); got != 4096 {
		t.Errorf("zero maxTokens should default to 4096, got %d", got)
	}
}

func TestApproxOutputTokensFloors(t *testing.T) {
	// Even an empty or tiny input incurs boilerplate JSON output —
	// 64-token floor keeps the estimate honest.
	if got := ApproxOutputTokens(0, 4096); got != 64 {
		t.Errorf("expected 64-token floor for zero input, got %d", got)
	}
	if got := ApproxOutputTokens(10, 4096); got != 64 {
		t.Errorf("expected 64-token floor for tiny input, got %d", got)
	}
}

func TestApproxOutputTokensScalesWithInput(t *testing.T) {
	// 1/8 of input above the floor — verifies the heuristic isn't
	// a constant.
	if got := ApproxOutputTokens(8000, 4096); got != 1000 {
		t.Errorf("expected 8000/8=1000, got %d", got)
	}
}

// stubWithoutCost / stubWithCost exercise the optional-interface
// pattern documented on CostEstimator. A backend that doesn't
// implement EstimateCost shouldn't accidentally satisfy the
// assertion (would cause a runtime panic in callers).

type stubWithoutCost struct{}

func (stubWithoutCost) Name() string { return "stub" }
func (stubWithoutCost) Generate(context.Context, Request) (Response, error) {
	return Response{}, nil
}

type stubWithCost struct{ rate float64 }

func (stubWithCost) Name() string { return "stub-priced" }
func (stubWithCost) Generate(context.Context, Request) (Response, error) {
	return Response{}, nil
}
func (s stubWithCost) EstimateCost(in, out int) float64 { return float64(in+out) * s.rate }

func TestCostEstimatorTypeAssertion(t *testing.T) {
	var c1 Client = stubWithoutCost{}
	if _, ok := c1.(CostEstimator); ok {
		t.Errorf("stub-without-cost should NOT satisfy CostEstimator")
	}
	var c2 Client = stubWithCost{rate: 0.01}
	est, ok := c2.(CostEstimator)
	if !ok {
		t.Fatalf("stub-with-cost should satisfy CostEstimator")
	}
	if got := est.EstimateCost(100, 50); got != 1.5 {
		t.Errorf("EstimateCost(100, 50) at rate 0.01 = %v, want 1.5", got)
	}
}
