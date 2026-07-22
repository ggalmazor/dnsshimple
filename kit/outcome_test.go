package kit

import (
	"errors"
	"strconv"
	"testing"
)

func TestChainFeedsASuccessIntoTheNextStep(test *testing.T) {
	// Given a successful Outcome carrying a number
	initial := Success(42)

	// When it is chained into a step that renders it
	rendered := Chain(initial, func(number int) Outcome[string] {
		return Success(strconv.Itoa(number))
	})

	// Then the step ran and its value came through
	if rendered.Failed() {
		test.Fatalf("chain failed unexpectedly: %v", rendered.Error())
	}
	if rendered.Value() != "42" {
		test.Fatalf("expected %q, got %q", "42", rendered.Value())
	}
}

func TestChainShortCircuitsPastRemainingStepsAfterAFailure(test *testing.T) {
	// Given a failed Outcome
	rootCause := errors.New("the upstream said no")
	failed := Failure[int](rootCause)

	// When it is chained into a step
	stepRan := false
	result := Chain(failed, func(int) Outcome[string] {
		stepRan = true
		return Success("never")
	})

	// Then the step never ran and the original failure survived the chain
	if stepRan {
		test.Fatal("a step ran after a failure; the railway must short-circuit")
	}
	if !result.Failed() || !errors.Is(result.Error(), rootCause) {
		test.Fatalf("expected the original failure to come through, got %v", result.Error())
	}
}
