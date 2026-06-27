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

// Run sends 10 pings at 200 ms intervals. Requires NET_RAW or root.
func Run(target string) (Result, error) {
	out, err := exec.Command("ping", "-c", "10", "-i", "0.2", "-W", "1", "-q", target).Output()
	if err != nil && len(out) == 0 {
		return Result{}, fmt.Errorf("ping exec: %w", err)
	}
	return Parse(out)
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
