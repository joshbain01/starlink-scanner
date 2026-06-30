package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"pp-starlink/internal/starlink"
)

type externalLocationPayload struct {
	Lat           *float64 `json:"lat"`
	Lon           *float64 `json:"lon"`
	AltitudeM     *float64 `json:"altitude_m"`
	Valid         *bool    `json:"valid"`
	Timestamp     string   `json:"timestamp"`
	TimestampUnix *int64   `json:"timestamp_unix"`
}

func fetchExternalLocation(ctx context.Context, command string) (starlink.Location, error) {
	out, err := exec.CommandContext(ctx, "sh", "-c", command).Output()
	if err != nil {
		return starlink.Location{}, fmt.Errorf("location command failed: %w", err)
	}
	var p externalLocationPayload
	if err := json.Unmarshal(out, &p); err != nil {
		return starlink.Location{}, fmt.Errorf("invalid location JSON: %w", err)
	}
	if p.Valid != nil && !*p.Valid {
		return starlink.Location{}, nil
	}
	if p.Lat == nil || p.Lon == nil {
		return starlink.Location{}, fmt.Errorf("location JSON missing lat/lon")
	}

	loc := starlink.Location{
		Lat:   *p.Lat,
		Lon:   *p.Lon,
		Valid: true,
	}
	if p.AltitudeM != nil {
		loc.AltitudeM = *p.AltitudeM
	}
	if p.TimestampUnix != nil {
		loc.Timestamp = time.Unix(*p.TimestampUnix, 0).UTC()
	} else if strings.TrimSpace(p.Timestamp) != "" {
		ts, err := time.Parse(time.RFC3339, p.Timestamp)
		if err != nil {
			return starlink.Location{}, fmt.Errorf("invalid timestamp: %w", err)
		}
		loc.Timestamp = ts.UTC()
	}
	return loc, nil
}
