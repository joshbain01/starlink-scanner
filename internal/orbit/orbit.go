package orbit

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	tleURL = "https://celestrak.org/NORAD/elements/gp.php?GROUP=starlink&FORMAT=TLE"
	tleTTL = 24 * time.Hour
)

// TLE holds the three-line element set for one satellite.
type TLE struct {
	Name, Line1, Line2 string
}

// LookAngle is the az/el of a satellite from an observer.
type LookAngle struct {
	SatID             string
	Azimuth, Elevation float64
}

// BadZone marks an az/el bucket center that historically has high packet loss.
type BadZone struct {
	Az, El float64
}

// RiskWindow is a contiguous predicted time range where a pass crosses a bad zone.
type RiskWindow struct {
	Start, End         time.Time
	SatID              string
	Azimuth, Elevation float64
}

// FetchTLEs returns cached Starlink TLEs, refreshing from CelesTrak when the cache is stale.
func FetchTLEs(cacheFile string) ([]TLE, error) {
	if info, err := os.Stat(cacheFile); err == nil && time.Since(info.ModTime()) < tleTTL {
		return parseTLEFile(cacheFile)
	}
	resp, err := http.Get(tleURL) //nolint:noctx
	if err != nil {
		if _, serr := os.Stat(cacheFile); serr == nil {
			return parseTLEFile(cacheFile) // fall back to stale cache
		}
		return nil, err
	}
	defer resp.Body.Close()
	f, err := os.Create(cacheFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return nil, err
	}
	return parseTLEFile(cacheFile)
}

func parseTLEFile(path string) ([]TLE, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var (
		tles  []TLE
		lines [3]string
		i     int
	)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		lines[i%3] = line
		if i%3 == 2 {
			tles = append(tles, TLE{Name: lines[0], Line1: lines[1], Line2: lines[2]})
		}
		i++
	}
	return tles, sc.Err()
}

// HighestSatellite returns the Starlink satellite with the highest elevation at t.
// ponytail: Keplerian only (no J2/drag), ~1° error/orbit; replace with SGP4 if accuracy matters
func HighestSatellite(tles []TLE, lat, lon float64, t time.Time) (LookAngle, error) {
	best := LookAngle{Elevation: -91}
	for _, tle := range tles {
		r, err := propagate(tle, t)
		if err != nil {
			continue
		}
		az, el := eciToAzEl(r, lat, lon, t)
		if el > best.Elevation {
			best = LookAngle{SatID: tle.Name, Azimuth: az, Elevation: el}
		}
	}
	if best.Elevation == -91 {
		return LookAngle{}, fmt.Errorf("no TLEs propagated successfully")
	}
	return best, nil
}

// PassesInWindow steps through dur at 1-minute resolution and returns time ranges where
// the highest satellite crosses a historically bad az/el zone (±5° az, ±2.5° el tolerance).
func PassesInWindow(tles []TLE, lat, lon float64, bad []BadZone, dur time.Duration) []RiskWindow {
	var (
		windows []RiskWindow
		current *RiskWindow
	)
	now := time.Now()
	for s := 0; s <= int(dur.Minutes()); s++ {
		t := now.Add(time.Duration(s) * time.Minute)
		look, err := HighestSatellite(tles, lat, lon, t)
		if err != nil {
			continue
		}
		inBad := false
		for _, z := range bad {
			if math.Abs(look.Azimuth-z.Az) <= 5 && math.Abs(look.Elevation-z.El) <= 2.5 {
				inBad = true
				break
			}
		}
		if inBad {
			if current == nil {
				w := RiskWindow{Start: t, SatID: look.SatID, Azimuth: look.Azimuth, Elevation: look.Elevation}
				current = &w
			}
			current.End = t
		} else if current != nil {
			windows = append(windows, *current)
			current = nil
		}
	}
	if current != nil {
		windows = append(windows, *current)
	}
	return windows
}

// --- orbital mechanics (Keplerian, two-body) ---

type elements struct {
	epoch       time.Time
	n           float64 // mean motion rad/min
	e, i, Ω, ω, M0 float64 // eccentricity and angles in radians
}

func parseTLE(tle TLE) (elements, error) {
	l1, l2 := tle.Line1, tle.Line2
	if len(l1) < 69 || len(l2) < 63 {
		return elements{}, fmt.Errorf("TLE lines too short")
	}
	ep, err := parseEpoch(strings.TrimSpace(l1[18:32]))
	if err != nil {
		return elements{}, err
	}
	// eccentricity: leading decimal assumed (e.g. "0034543" → 0.0034543)
	ecc := mustFloat("0." + strings.TrimSpace(l2[26:33]))
	// mean motion: rev/day → rad/min
	n := mustFloat(strings.TrimSpace(l2[52:63])) * 2 * math.Pi / (24 * 60)
	return elements{
		epoch: ep,
		n:     n,
		e:     ecc,
		i:     deg2rad(mustFloat(strings.TrimSpace(l2[8:16]))),
		Ω:     deg2rad(mustFloat(strings.TrimSpace(l2[17:25]))),
		ω:     deg2rad(mustFloat(strings.TrimSpace(l2[34:42]))),
		M0:    deg2rad(mustFloat(strings.TrimSpace(l2[43:51]))),
	}, nil
}

func parseEpoch(s string) (time.Time, error) {
	if len(s) < 5 {
		return time.Time{}, fmt.Errorf("epoch too short: %q", s)
	}
	yy, err := strconv.Atoi(s[:2])
	if err != nil {
		return time.Time{}, err
	}
	year := 2000 + yy
	if yy >= 57 {
		year = 1900 + yy
	}
	doy, err := strconv.ParseFloat(s[2:], 64)
	if err != nil {
		return time.Time{}, err
	}
	jan1 := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	return jan1.Add(time.Duration((doy - 1) * float64(24*time.Hour))), nil
}

func mustFloat(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }
func deg2rad(d float64) float64  { return d * math.Pi / 180 }

// propagate returns the satellite ECI position (km) at time t using Keplerian mechanics.
func propagate(tle TLE, t time.Time) ([3]float64, error) {
	el, err := parseTLE(tle)
	if err != nil {
		return [3]float64{}, err
	}
	dt := t.Sub(el.epoch).Minutes()
	M := math.Mod(el.M0+el.n*dt, 2*math.Pi)
	if M < 0 {
		M += 2 * math.Pi
	}
	// Newton's method on Kepler's equation: E - e*sin(E) = M
	E := M
	for k := 0; k < 5; k++ {
		E -= (E - el.e*math.Sin(E) - M) / (1 - el.e*math.Cos(E))
	}
	// true anomaly
	sE, cE := math.Sin(E), math.Cos(E)
	nu := math.Atan2(math.Sqrt(1-el.e*el.e)*sE, cE-el.e)

	// semi-major axis from mean motion (µ = 398600.4418 km³/s²)
	nSec := el.n / 60
	a := math.Cbrt(398600.4418 / (nSec * nSec))

	// perifocal position
	r := a * (1 - el.e*cE)
	px, py := r*math.Cos(nu), r*math.Sin(nu)

	// rotate perifocal → ECI using Ω, i, ω
	cΩ, sΩ := math.Cos(el.Ω), math.Sin(el.Ω)
	ci, si := math.Cos(el.i), math.Sin(el.i)
	cω, sω := math.Cos(el.ω), math.Sin(el.ω)

	x := (cΩ*cω-sΩ*sω*ci)*px + (-cΩ*sω-sΩ*cω*ci)*py
	y := (sΩ*cω+cΩ*sω*ci)*px + (-sΩ*sω+cΩ*cω*ci)*py
	z := (sω*si)*px + (cω*si)*py

	return [3]float64{x, y, z}, nil
}

// eciToAzEl converts an ECI position (km) to azimuth/elevation (degrees) from an observer.
func eciToAzEl(eci [3]float64, latDeg, lonDeg float64, t time.Time) (az, el float64) {
	gmst := calcGMST(t)
	cg, sg := math.Cos(gmst), math.Sin(gmst)

	// ECI → ECEF (rotate around Z by -GMST)
	ex := cg*eci[0] + sg*eci[1]
	ey := -sg*eci[0] + cg*eci[1]
	ez := eci[2]

	// observer ECEF (spherical Earth, ~0.3% flattening error; fine for ±1° accuracy)
	lat := deg2rad(latDeg)
	lon := deg2rad(lonDeg)
	const re = 6371.0
	clat, slat := math.Cos(lat), math.Sin(lat)
	clon, slon := math.Cos(lon), math.Sin(lon)
	ox := re * clat * clon
	oy := re * clat * slon
	oz := re * slat

	dx, dy, dz := ex-ox, ey-oy, ez-oz
	rng := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if rng < 1 {
		return 0, 0
	}

	// ECEF range vector → ENU
	east  := -slon*dx + clon*dy
	north := -slat*clon*dx - slat*slon*dy + clat*dz
	up    :=  clat*clon*dx + clat*slon*dy + slat*dz

	el = math.Asin(up/rng) * 180 / math.Pi
	az = math.Atan2(east, north) * 180 / math.Pi
	if az < 0 {
		az += 360
	}
	return az, el
}

// calcGMST returns Greenwich Mean Sidereal Time in radians (Vallado 2006 simplified).
func calcGMST(t time.Time) float64 {
	j2000 := time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)
	d := t.UTC().Sub(j2000).Hours() / 24.0
	deg := math.Mod(280.46061837+360.98564736629*d, 360)
	if deg < 0 {
		deg += 360
	}
	return deg2rad(deg)
}
