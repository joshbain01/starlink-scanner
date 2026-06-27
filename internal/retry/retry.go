package retry

// Policy decides whether to retry and what state to track.
type Policy interface {
	// ShouldRedial reports true when consecutive failures exceed the threshold.
	ShouldRedial(consecutiveFails int) bool
}

// ConsecutiveFailures fires a redial after N consecutive failures.
type ConsecutiveFailures struct{ N int }

func (p ConsecutiveFailures) ShouldRedial(n int) bool { return n >= p.N }
