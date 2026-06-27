package ping

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type Result struct {
	AvgLatencyMs float64
	JitterMs     float64 // mdev from ping summary
	PacketLoss   float64 // 0.0–1.0
}

// ParseError is returned when ping output cannot be parsed.
type ParseError struct {
	Msg string
	Raw string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("ping parse: %s (raw: %q)", e.Msg, e.Raw)
}

// Pinger is the interface callers should depend on.
type Pinger interface {
	Run(target string) (Result, error)
}

// CommandPinger shells out to the system ping binary.
type CommandPinger struct{}

// Run sends 10 pings at 200 ms intervals. Requires NET_RAW or root.
func (CommandPinger) Run(target string) (Result, error) {
	out, err := exec.Command("ping", "-c", "10", "-i", "0.2", "-W", "1", "-q", target).Output()
	if err != nil && len(out) == 0 {
		return Result{}, fmt.Errorf("ping exec: %w", err)
	}
	return Parse(out)
}

// defaultPinger is the package-level singleton used by the top-level Run func.
var defaultPinger Pinger = CommandPinger{}

// Run is a package-level convenience wrapper that delegates to CommandPinger.
// Kept for backwards compatibility with existing callers.
func Run(target string) (Result, error) {
	return defaultPinger.Run(target)
}

// MockPinger returns canned results for use in tests.
type MockPinger struct {
	Results map[string]Result
	Errors  map[string]error
}

func (m *MockPinger) Run(target string) (Result, error) {
	if m.Errors != nil {
		if err, ok := m.Errors[target]; ok {
			return Result{}, err
		}
	}
	if m.Results != nil {
		if r, ok := m.Results[target]; ok {
			return r, nil
		}
	}
	return Result{}, nil
}

// Parse extracts jitter, average latency, and packet loss from Linux ping -q output:
//
//	"10 packets transmitted, 8 received, 20% packet loss"
//	"rtt min/avg/max/mdev = 1.234/2.345/3.456/0.456 ms"
func Parse(out []byte) (Result, error) {
	var r Result
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "packet loss") {
			for _, f := range strings.Fields(line) {
				if strings.HasSuffix(f, "%") {
					pct := strings.TrimSuffix(f, "%")
					v, err := strconv.ParseFloat(pct, 64)
					if err != nil {
						return r, fmt.Errorf("parse loss %q: %w", pct, err)
					}
					r.PacketLoss = v / 100.0
				}
			}
		}
		if strings.HasPrefix(line, "rtt") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) < 2 {
				continue
			}
			nums := strings.Split(strings.TrimSpace(strings.Fields(parts[1])[0]), "/")
			if len(nums) < 4 {
				continue
			}
			if avg, err := strconv.ParseFloat(nums[1], 64); err == nil {
				r.AvgLatencyMs = avg
			}
			if mdev, err := strconv.ParseFloat(nums[3], 64); err == nil {
				r.JitterMs = mdev
			}
		}
	}
	return r, nil
}
