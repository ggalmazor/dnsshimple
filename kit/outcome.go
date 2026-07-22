// Package kit holds the small shared primitives of the application core.
package kit

// Outcome is a minimal Result monad: a computation that either produced a
// Value or failed with an error. Business steps in the core return one value
// (an Outcome) instead of Go's (value, error) pair; adapters unwrap at the
// boundary (DESIGN.md §12).
type Outcome[Value any] struct {
	value   Value
	failure error
}

// Nothing is the unit value for Outcomes of effect-only steps, so they still
// ride the railway without inventing a meaningless payload.
type Nothing struct{}

func Done() Outcome[Nothing] {
	return Success(Nothing{})
}

func Success[Value any](value Value) Outcome[Value] {
	return Outcome[Value]{value: value}
}

func Failure[Value any](failure error) Outcome[Value] {
	return Outcome[Value]{failure: failure}
}

func (outcome Outcome[Value]) Failed() bool { return outcome.failure != nil }
func (outcome Outcome[Value]) Error() error { return outcome.failure }
func (outcome Outcome[Value]) Value() Value { return outcome.value }

// Chain feeds a successful Outcome into the next step; a failure
// short-circuits past every remaining step (railway style). It is a free
// function because Go methods cannot introduce new type parameters.
func Chain[From, To any](previous Outcome[From], step func(From) Outcome[To]) Outcome[To] {
	if previous.Failed() {
		return Failure[To](previous.failure)
	}
	return step(previous.value)
}
