package maintenance

import "time"

// Strategy is implemented by any type that knows when and how to run a
// periodic maintenance operation. Implement it to override schedules or
// inject no-ops in tests.
type Strategy interface {
	ShouldRun(now time.Time, lastRun time.Time) bool
	Run() error
}

// Interval is a maintenance strategy that runs at most once per given period.
type Interval struct {
	Period  time.Duration
	RunFunc func() error
}

func (s *Interval) ShouldRun(now time.Time, lastRun time.Time) bool {
	return now.Sub(lastRun) >= s.Period
}

func (s *Interval) Run() error { return s.RunFunc() }

// Orchestrator runs a slice of strategies in order, tracking last-run times.
type Orchestrator struct {
	strategies []Strategy
	lastRun    []time.Time
}

func New(strategies ...Strategy) *Orchestrator {
	last := make([]time.Time, len(strategies))
	return &Orchestrator{strategies: strategies, lastRun: last}
}

// Tick checks each strategy and runs any that are due. Errors are logged by the
// caller — Tick continues past errors.
func (o *Orchestrator) Tick(now time.Time) []error {
	var errs []error
	for i, s := range o.strategies {
		if s.ShouldRun(now, o.lastRun[i]) {
			if err := s.Run(); err != nil {
				errs = append(errs, err)
			}
			o.lastRun[i] = now
		}
	}
	return errs
}
