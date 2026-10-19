// `tithi` computes the current lunar day and tithi for a given
// geographical location and current time.
//
// A lunar day is the interval between a new moon and the next moon rise,
// or between two consecutive moon rises, or between the last moon rise
// and the new moon. There are 29 or 30 lunar days between two new moons.
//
// A tithi is the interval during which the elongation of the Moon from
// increases by 12°. There are exactly 30 tithis in a synodic (lunar) month,
// numbered 1..15 in the waxing fortnight (Śukla pakṣa) and 16..30 in the
// waning fortnight (Kṛṣṇa pakṣa).
package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/pietv/tithi/tzgeo"

	"github.com/charmbracelet/lipgloss"
	"github.com/soniakeys/meeus/v3/base"
	"github.com/soniakeys/meeus/v3/coord"
	"github.com/soniakeys/meeus/v3/deltat"
	"github.com/soniakeys/meeus/v3/globe"
	"github.com/soniakeys/meeus/v3/julian"
	"github.com/soniakeys/meeus/v3/moonphase"
	"github.com/soniakeys/meeus/v3/moonposition"
	"github.com/soniakeys/meeus/v3/nutation"
	"github.com/soniakeys/meeus/v3/rise"
	"github.com/soniakeys/meeus/v3/sidereal"
	"github.com/soniakeys/meeus/v3/solar"
	"github.com/soniakeys/unit"
	"github.com/xo/terminfo"
	"golang.org/x/term"
)

// tithiName returns a tithi day name and the period (pakṣa) for a tithi
// number (1..30), where 1 is the first tithi of Śukla pakṣa.
func tithiName(k int) (day, paksha string) {
	if k < 1 || k > 30 {
		panic("k must be between 1 and 30")
	}
	paksha = "Śukla pakṣa"
	if k > 15 {
		paksha = "Kṛṣṇa pakṣa"
	}
	return [...]string{
		// Waxing crescent tithi 1..15.
		"Pratipadā", "Dvitīyā", "Tṛtīyā", "Caturthī",
		"Pañcamī", "Ṣaṣṭhī", "Saptamī", "Aṣṭamī",
		"Navamī", "Daśamī", "Ekādaśī", "Dvādaśī",
		"Trayodaśī", "Caturdaśī", "Pūrṇimā",

		// Waning crescent tithi 16..30.
		"Pratipadā", "Dvitīyā", "Tṛtīyā", "Caturthī",
		"Pañcamī", "Ṣaṣṭhī", "Saptamī", "Aṣṭamī",
		"Navamī", "Daśamī", "Ekādaśī", "Dvādaśī",
		"Trayodaśī", "Caturdaśī", "Amāvāsyā",
	}[k-1], paksha
}

// jdToJDE converts a UT-based Julian Day to a Julian Ephemeris Day (TT)
// by adding ΔT. Yearly-granularity ΔT is sufficient for all ephemeris
// computations in this program (≪1s impact on tithi boundary times).
func jdToJDE(jd float64) float64 {
	y, m, _ := julian.JDToCalendar(jd)
	yearFrac := float64(y) + (float64(m)-0.5)/12
	dT := deltat.PolyAfter2000(yearFrac).Sec()
	return jd + dT/86400
}

// elongation returns (λMoon − λSun) mod 360, in degrees, at UTC instant t.
// This quantity is the fundamental input for tithi arithmetic:
// tithi index = floor(elongation / 12).
//
// # Timescale conversion (UT → TT)
//
// Planetary theories use Terrestrial Time (TT), not Universal Time (UT).
// Their difference ΔT = TT − UT grows slowly (≈70s in 2026).  We add
// ΔT (converted to days) to the UT Julian Day to obtain the Julian
// Ephemeris Day (JDE), the correct time argument for all ephemeris and
// nutation series.  Yearly-granularity ΔT is sufficient here because
// the sub-year uncertainty in ΔT shifts elongation by far less than 1″.
//
// # Sun's apparent ecliptic longitude
//
// solar.ApparentLongitude computes the Sun's geometric longitude via the
// VSOP87 model and applies two corrections to produce the apparent longitude
// referenced to the true equinox of date:
//
//   - Nutation in longitude (Δψ): an 18.6-year wobble of the ecliptic plane
//     caused by the Moon and Sun pulling on Earth's equatorial bulge.  The
//     IAU 1980 series (106 periodic terms) gives Δψ with peak amplitude ≈17.2″.
//
//   - Annual aberration: the apparent ≈20.5″ shift caused by the finite speed
//     of light combined with Earth's orbital velocity (≈30 km/s).
//
// # Moon's apparent ecliptic longitude
//
// moonposition.Position (Meeus Ch. 47) returns the Moon's geocentric ecliptic
// longitude λ in the mean equinox of date — a frame not yet corrected for
// nutation.  nutation.Nutation applies the same IAU 1980 series to compute
// Δψ (nutation in longitude).  Adding Δψ converts λ from the mean to the
// true (apparent) equinox, placing Moon and Sun in the same reference frame.
//
// The Moon's light-time aberration (~0.7″) is neglected; at 12°/day it would
// shift a tithi boundary by less than 8 seconds.
//
// # Elongation
//
// The raw difference (λMoon − λSun) is reduced modulo 360 to [0, 360),
// so a waxing crescent reads near 0° and full Moon near 180°.
func elongation(t time.Time) float64 {
	jde := jdToJDE(julian.TimeToJD(t.UTC()))

	T := base.J2000Century(jde)
	lSun := solar.ApparentLongitude(T).Deg()
	lMoonMean, _, _ := moonposition.Position(jde)
	dpsi, _ := nutation.Nutation(jde)
	lMoon := (lMoonMean + dpsi).Deg()

	d := math.Mod(lMoon-lSun, 360)
	if d < 0 {
		d += 360
	}
	return d
}

// moonEquatorial returns the Moon's right ascension α, declination δ, and
// geocentric distance Δ (km) for the given Julian Day (UT).
//
// # Why equatorial coordinates?
//
// rise.Times (Meeus Ch. 15) works in the equatorial frame — it needs α and δ
// for three consecutive days so it can interpolate the Moon's position to the
// exact moment its altitude equals the effective horizon.  moonposition.Position
// yields ecliptic coordinates, so we rotate them to equatorial here.
//
// # Timescale (UT → TT)
//
// ΔT converts the UT Julian Day to the TT-based JDE needed by the Moon ephemeris
// and nutation series (same reasoning as in elongation).
//
// # Moon's ecliptic position
//
// moonposition.Position returns geocentric ecliptic longitude λ (mean equinox of
// date), latitude β, and distance Δ in km.
//
// # True obliquity of the ecliptic
//
// Rotating ecliptic coordinates to equatorial requires the angle ε between the
// ecliptic and the celestial equator.  It has two contributions:
//
//   - Mean obliquity ε₀: a slowly changing polynomial in T (Meeus eq. 22.3),
//     currently ≈23°26′, decreasing at ≈47″/century.
//
//   - Nutation in obliquity Δε: the short-period component from the IAU 1980
//     series, peak ≈9.2″ with a period of 18.6 years.
//
// Their sum ε = ε₀ + Δε is the true obliquity used for the rotation.
// Note: nutation in longitude Δψ is deliberately discarded here; converting
// the mean-equinox λ with the true obliquity is a common approximation that
// remains within ~1″ of the fully apparent position — well within the
// ≈1 min accuracy achievable for rise/set times.
//
// # Ecliptic → equatorial rotation
//
// coord.EclToEq applies the standard spherical-trig rotation (Meeus §13):
//
//	tan α = (sin λ · cos ε − tan β · sin ε) / cos λ
//	sin δ =  sin β · cos ε + cos β · sin ε · sin λ
//
// The distance Δ is returned unchanged; the caller uses it to compute the
// Moon's horizontal parallax for the effective horizon altitude in rise.Times.
func moonEquatorial(jd float64) (unit.RA, unit.Angle, float64) {
	jde := jdToJDE(jd)

	λ, β, Δ := moonposition.Position(jde)
	_, Δε := nutation.Nutation(jde)
	ε := nutation.MeanObliquity(jde) + Δε
	sε, cε := ε.Sincos()
	α, δ := coord.EclToEq(λ, β, sε, cε)
	return α, δ, Δ
}

// moonrise computes the moonrise time for the UTC calendar date
// of probe (which must be a UTC midnight), for an observer at lat/lon
// location. Returns time in UTC. Returns an error (e.g. rise.ErrorCircumpolar,
// rise.ErrorNeverRises) when the moon does not cross the effective horizon
// on that date at that latitude.
//
// The algorithm is Meeus Ch. 15: the crude estimate of rise.ApproxTimes,
// refined by the altitude correction (15.4a/15.4b) run to convergence on
// the true ephemeris.
//
// # Julian Day (jd)
//
// julian.CalendarGregorianToJD converts a Gregorian calendar date to a
// Julian Day — a continuous count of solar days since noon on
// 1 January 4713 BCE. All astronomical series in the ‘meeus’ package accept
// JD as the time argument. The day component is passed as a float64 whose
// fractional part encodes the time of day; an integer value (e.g. 15.0)
// means 0h UT of that date and yields a JD ending in .5, because whole JD
// numbers fall on noon. jd here is therefore the UTC midnight in probe.
//
// # Observer position (p)
//
// globe.Coord holds the observer's geographic latitude and longitude.
// The meeus library follows the historical astronomical convention of
// west-positive longitude, so the east-positive lon argument is negated.
//
// # Greenwich Apparent Sidereal Time at 0h UT (Th0)
//
// sidereal.Apparent0UT(jd) returns the apparent Greenwich Sidereal Time
// (GAST) at the start of the UT day — i.e. the sidereal time at the
// Greenwich meridian when the UT clock reads 00:00. The rise/set algorithm
// uses GAST as the starting reference for local hour-angle calculations:
// adding the observer's geographic longitude converts GAST to local sidereal
// time, which in turn gives the Moon's hour angle at any UT instant.
//
// # Effective horizon altitude (h0)
//
// For a point source the geometric horizon is 0°, but the Moon has an
// appreciable disk and the atmosphere bends light upward. The standard
// altitude h0 combines both effects:
//
//	moonposition.Parallax(Δ) computes the Moon's horizontal parallax
//	π = arcsin(6378.14 km / Δ).  The parallax is large (≈57′) because
//	the Moon is nearby; it depresses the apparent position relative to
//	the geometric direction to the Moon.
//
//	rise.Stdh0Lunar(π) = 0.7275·π − 0°33′40″ combines that downward
//	parallax displacement with the upward correction for atmospheric
//	refraction, yielding the altitude at which the Moon's limb appears
//	to touch the geometric horizon.
//
// # rise.ApproxTimes
//
// rise.ApproxTimes returns (tRise, tTransit, tSet, error); only tRise is
// used.  It is a unit.Time value representing elapsed time since 0h UT of
// the probe date, computed from the rise/set geometry alone (15.1, 15.2)
// with the Moon frozen at its 0h position — crude for a body moving
// ≈12°/day, hence the refinement below.  Calling .Sec() converts a
// unit.Time to seconds; multiplying by float64(time.Second) (nanoseconds
// per second) and wrapping in time.Duration gives the offset from the UTC
// midnight in probe, producing an absolute UTC timestamp for moonrise.
//
// # Refinement to convergence — and why not rise.Times
//
// The correction step below is the one rise.Times applies internally
// (Meeus 15.4a/15.4b): evaluate the Moon's altitude h at the current
// estimate m, and convert the miss h − h0 into a time step via the rate
// at which the hour angle H sweeps altitude, sH·cδ·cLat.  Advancing the
// sidereal time uses the ratio of the solar to the sidereal day
// (360.985647°/360°, Meeus p. 103).  rise.Times itself is avoided for two
// reasons:
//
//   - It applies the correction exactly once.  For slow movers (stars,
//     planets, the Sun) one step lands within seconds, but for the fast
//     Moon a single linear step consistently undershoots, leaving a
//     residual on the order of minutes.  Meeus says to repeat the
//     correction "if sufficient accuracy is not attained" (Ch. 15, p.103),
//     so here it runs to convergence (<0.5s change).  A step still that
//     large after 10 rounds means the correction is oscillating rather
//     than settling — at high latitudes the Moon can merely graze the
//     effective horizon, giving a near-tangent crossing (or none at all)
//     with no stable root — and ErrNoConvergence is returned instead of
//     a fabricated time.
//
//   - It approximates α and δ by three-point Lagrange interpolation over
//     the day before/of/after — an artifact of working from daily almanac
//     tables, and one with its own failure mode: α wraps at 0h/24h, and
//     the samples must be unwrapped before interpolating or a crossing
//     inside the 3-day window corrupts the fit. moonEquatorial
//     can be evaluated at any instant, so each iteration just computes the
//     true position at jd + m (ΔT is applied inside moonEquatorial); α
//     then enters only through the sine and cosine of H, where wrapping
//     is harmless.
func moonrise(probe time.Time, lat, lon float64) (time.Time, error) {
	py, pm, pd := probe.Date()

	var (
		jd = julian.CalendarGregorianToJD(py, int(pm), float64(pd))

		p = globe.Coord{
			Lat: unit.AngleFromDeg(lat),
			// Globe uses the west-positive convention.
			Lon: unit.AngleFromDeg(-lon),
		}
		Th0 = sidereal.Apparent0UT(jd)
	)
	α, δ, Δ := moonEquatorial(jd)
	h0 := rise.Stdh0Lunar(moonposition.Parallax(Δ))
	m, _, _, err := rise.ApproxTimes(p, h0, Th0, α, δ)
	if err != nil {
		return time.Time{}, err
	}

	sLat, cLat := p.Lat.Sincos()
	for range 10 {
		th0 := (Th0 + m.Mul(360.985647/360)).Mod1()
		α, δ, _ := moonEquatorial(jd + m.Day())
		sδ, cδ := δ.Sincos()
		sH, cH := math.Sincos(th0.Rad() - p.Lon.Rad() - α.Rad())
		h := math.Asin(sLat*sδ + cLat*cδ*cH)
		md := (unit.TimeFromRad(h) - h0.Time()).Div(cδ * cLat * sH)
		m += md
		if math.Abs(md.Sec()) < 0.5 {
			return probe.Add(time.Duration(m.Sec() * float64(time.Second))), nil
		}
	}
	return time.Time{}, ErrNoConvergence
}

// ErrNoConvergence is returned by moonrise when the iterated altitude
// correction oscillates instead of settling, meaning the Moon only grazes
// the effective horizon on the probed date and no reliable rise time exists.
var ErrNoConvergence = errors.New("altitude correction did not converge")

// Interval is a lunar interval for a (one-based) lunar day or a tithi.
type Interval struct {
	Number     int
	Start, End time.Time
}

// lunarSessions returns the lunar days and tithis overlapping the local
// solar day [dayStart, dayStart+24h) in chronological order, for an
// observer at lat/lon (degrees, +N/+E). dayStart must be a local midnight.
//
// Both kinds of intervals are anchored at the last new moon before dayStart:
// a new moon starts lunar day 1 and tithi 1, so walking boundary to boundary
// from the anchor numbers every interval along the way.
//
// The walk extends 48 hours past today's end — longer than any boundary spacing
// (a tithi lasts ≤ 27h, a lunar day ≤ ~26h) — so the intervals still in progress
// at day's end are closed by their real end boundary.
func lunarSessions(dayStart time.Time, lat, lon float64) (lunarDays, tithis []Interval) {
	var (
		dayEnd = dayStart.Add(24 * time.Hour)
		limit  = dayEnd.Add(48 * time.Hour)
		anchor = newMoonBefore(dayStart)
	)

	// Lunar days. Gather every boundary in [anchor, limit): the new moons
	// and the moonrises. moonrise is probed once per UTC calendar day, so
	// the rises come out in order (real moonrises are ≈25h apart) except
	// that two adjacent probes can report the same moonrise twice;
	// a rise within an hour of the previously kept one is dropped.
	type boundary struct {
		at      time.Time
		newMoon bool
	}
	bounds := []boundary{{anchor, true}}
	for nm := anchor; nm.Before(limit); {
		nm = newMoonAfter(nm)
		bounds = append(bounds, boundary{nm, true})
	}
	var lastRise time.Time
	for probe := startOfDay(anchor, time.UTC); probe.Before(limit); probe = probe.AddDate(0, 0, 1) {
		mr, err := moonrise(probe, lat, lon)
		if err == nil && mr.After(anchor) && mr.Before(limit) &&
			(lastRise.IsZero() || mr.Sub(lastRise).Abs() >= time.Hour) {
			bounds = append(bounds, boundary{mr, false})
			lastRise = mr
		}
	}
	sort.Slice(bounds, func(i, j int) bool { return bounds[i].at.Before(bounds[j].at) })

	overlapsToday := func(start, end time.Time) bool {
		return start.Before(dayEnd) && end.After(dayStart)
	}

	// Successive boundaries delimit the lunar days; numbering restarts at 1
	// on each new moon.
	for number, i := 0, 0; i+1 < len(bounds); i++ {
		if bounds[i].newMoon {
			number = 1
		} else {
			number++
		}
		if overlapsToday(bounds[i].at, bounds[i+1].at) {
			lunarDays = append(lunarDays, Interval{number, bounds[i].at, bounds[i+1].at})
		}
	}

	// Tithis. The anchor new moon is itself a tithi boundary (the 0°
	// elongation crossing), so successive boundaries from it delimit the
	// tithis. Each is numbered from the elongation at its midpoint, far
	// from any numerical jitter at the boundaries.
	for start := anchor; start.Before(dayEnd); {
		end := nextTithiBoundary(start)
		if overlapsToday(start, end) {
			tithis = append(tithis, Interval{tithi(start.Add(end.Sub(start)/2)) + 1, start, end})
		}
		start = end
	}
	return lunarDays, tithis
}

// startOfDay returns the local midnight of the civil day containing t.
func startOfDay(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// newMoonJDEtoUT converts a moonphase JDE result (TT-based) to a UTC
// time.Time, subtracting ΔT for the approximate year.
func newMoonJDEtoUT(jde, approxYear float64) time.Time {
	dT := deltat.PolyAfter2000(approxYear).Sec()
	return julian.JDToTime(jde - dT/86400)
}

// decimalYear converts t to the fractional-year representation expected by
// moonphase.* (e.g. 2026.29 for ~mid-April 2026).  Precision of ±1 day is
// sufficient, since moonphase only uses it to select the lunation number k.
func decimalYear(t time.Time) float64 {
	return float64(t.Year()) + (float64(t.YearDay())-1)/365.25
}

// newMoonBefore returns the latest new moon strictly before t. We seed the
// decimal-year search half a synodic month *before* t so that moonphase.New
// selects the previous lunation, guaranteeing a different instant from any
// new-moon-valued t we may be passed — otherwise floating-point noise in
// JDE→UT can produce a near-duplicate and corrupt downstream per-cycle logic.
func newMoonBefore(t time.Time) time.Time {
	y := decimalYear(t.Add(-14 * 24 * time.Hour))
	for {
		nm := newMoonJDEtoUT(moonphase.New(y), y)
		if nm.Before(t) {
			return nm
		}
		// One synodic month.
		y -= 0.0808
	}
}

// newMoonAfter returns the earliest new moon strictly after t.
func newMoonAfter(t time.Time) time.Time {
	y := decimalYear(t.Add(14 * 24 * time.Hour))
	for {
		nm := newMoonJDEtoUT(moonphase.New(y), y)
		if nm.After(t) {
			return nm
		}
		// One synodic month.
		y += 0.0808
	}
}

// tithi returns the absolute tithi index (0..29) at time t (in UTC).
func tithi(t time.Time) int { return int(math.Floor(elongation(t) / 12)) }

// nextTithiBoundary returns the first instant after t at which the tithi
// index changes, for a t that is itself a tithi boundary (within a few
// minutes; callers chain from the anchor new moon, which is the 0°
// elongation crossing). A tithi lasts 20–27 hours, so the next boundary
// lies inside (t+30min, t+30h); bisection over that bracket finds it,
// because tithi() — the floor of a monotonically increasing elongation over
// 12° — changes exactly once there.
//
// The 30-minute pad keeps numerical jitter at t itself from being mistaken
// for the next boundary. Sub-second precision is overkill for display purposes
// but keeps callers free of off-by-one artifacts around the boundary.
func nextTithiBoundary(t time.Time) time.Time {
	lo, hi := t.Add(30*time.Minute), t.Add(30*time.Hour)
	k := tithi(lo)
	for hi.Sub(lo) > time.Second {
		mid := lo.Add(hi.Sub(lo) / 2)
		if tithi(mid) == k {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi
}

// parseLocation converts an IANA timezone name ("Asia/Kolkata") into
// *time.Location, or empty for system timezone.
func parseLocation(tz string) (*time.Location, error) {
	if tz == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("cannot parse timezone %q", tz)
	}
	return loc, nil
}

//go:embed moon-images/*.txt
var moonImages embed.FS

// asciiMoon returns an 36⨉20 ASCII art moon image for the lunar day n.
// Lunar days without a picture (1 and 27..30) are shown as a black square.
func asciiMoon(n int) string {
	const width, height = 36, 20
	if b, err := moonImages.ReadFile(fmt.Sprintf("moon-images/%d.txt", n)); err == nil {
		return strings.TrimRight(string(b), "\n")
	}
	row := strings.Repeat(" ", width)
	return strings.TrimRight(strings.Repeat(row+"\n", height), "\n")
}

const (
	dateLayout  = "2006-Jan-2"
	shortLayout = "Jan _2 15:04 MST"

	inputLayout1 = "2006 Jan 2"
	inputLayout2 = "Jan 2"
	inputLayout3 = "2 Jan"
)

func main() {
	latFlag := flag.Float64("lat", math.NaN(), "latitude in degrees (+N, -S); default: current timezone")
	lonFlag := flag.Float64("lon", math.NaN(), "longitude in degrees (+E, -W); default: current timezone")
	tzFlag := flag.String("tz", "", fmt.Sprintf("timezone: IANA name, or empty for system (TZ=%q)", os.Getenv("TZ")))
	dateFlag := flag.String("date", "", "date other than today (in format: “2026 Jan 01”, “Jan 01”, or “1 Jan”)")
	flag.Usage = func() {
		program := filepath.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage: %s [-tz <zone>] [-lat <deg>] [-lon <deg>] [-date DATE]\n", program)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "Print the lunar day for a specified date and location (print for today\n")
		fmt.Fprintf(os.Stderr, "and for the location set in the ‘TZ’ environment variable by default.)\n")
		fmt.Fprintln(os.Stderr)
		flag.PrintDefaults()

	}
	flag.Parse()

	loc, err := parseLocation(*tzFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// Resolve an IANA zone name suitable for tzgeo lookup.
	zoneName := loc.String()
	if *tzFlag != "" {
		zoneName = *tzFlag
	}
	var ianaName string
	if zLat, zLon, ok := tzgeo.Lookup(zoneName); ok {
		// Label the location only when both coordinates come from the zone.
		if math.IsNaN(*latFlag) && math.IsNaN(*lonFlag) {
			ianaName = zoneName
		}
		if math.IsNaN(*latFlag) {
			*latFlag = zLat
		}
		if math.IsNaN(*lonFlag) {
			*lonFlag = zLon
		}
	}

	// Fallback: explicit $TZ env var, then hardcoded default.
	for _, name := range []string{os.Getenv("TZ"), "Europe/Moscow"} {
		if name == "" || !math.IsNaN(*latFlag) || !math.IsNaN(*lonFlag) {
			break
		}
		if zLat, zLon, ok := tzgeo.Lookup(name); ok {
			*latFlag, *lonFlag = zLat, zLon
			if ianaName == "" {
				ianaName = name
			}
		}
	}
	if math.IsNaN(*latFlag) || math.IsNaN(*lonFlag) {
		panic("could not determine coordinates from timezone")
	}

	date := time.Now()
	if *dateFlag != "" {
		var (
			currentYear = date.Year()
			matched     bool
		)
		for i, layout := range []string{inputLayout1, inputLayout2, inputLayout3} {
			date, err = time.ParseInLocation(layout, *dateFlag, loc)
			if err != nil {
				continue
			}
			matched = true

			// Supply the current year for parsed dates without a year.
			if i > 0 {
				date = date.AddDate(currentYear, 0, 0)
			}
			break
		}
		if !matched {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	const tmpl = `{{define "header"}}Date: {{.Day}}  Location: -lat={{fmtlat .Lat}} -lon={{fmtlon .Lon}}{{with .IANA}} -tz={{printf "%q" .}}{{end}}{{end -}}
{{define "lunar"}}Lunar Days{{range .LunarDays}}

{{hl (current .) (printf "%d Lunar Day:\n%s\n%s" .Number (fmttime "start" .Start) (fmttime "end" .End))}}{{end}}{{end -}}
{{define "tithis"}}Tithis{{range .Tithis}}

{{hl (current .) (printf "%d %s (%s):\n%s\n%s" .Number (name .Number) (paksha .Number) (fmttime "start" .Start) (fmttime "end" .End))}}{{end}}{{end}}`

	dayStart := startOfDay(date.In(loc), loc)
	lunarDays, tithis := lunarSessions(dayStart, *latFlag, *lonFlag)

	data := struct {
		Lat, Lon          float64
		IANA              string
		Day               string
		LunarDays, Tithis []Interval
	}{
		Lat:       *latFlag,
		Lon:       *lonFlag,
		IANA:      ianaName,
		Day:       dayStart.Format(dateLayout),
		LunarDays: lunarDays,
		Tithis:    tithis,
	}
	now := time.Now()

	// Terminal color capabilities.
	var reverse, reset string
	if term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == "" {
		if info, err := terminfo.LoadFromEnv(); err == nil {
			reverse = info.Printf(terminfo.EnterBoldMode) + info.Printf(terminfo.EnterReverseMode)
			reset = info.Printf(terminfo.ExitAttributeMode)
		}
	}

	tpl := template.Must(template.New("report").Funcs(template.FuncMap{
		"fmtlat": func(lat float64) string {
			if lat >= 0 {
				return fmt.Sprintf("%.4f°N", lat)
			}
			return fmt.Sprintf("%.4f°S", lat)
		},
		"fmtlon": func(lon float64) string {
			if lon >= 0 {
				return fmt.Sprintf("%.4f°E", lon)
			}
			return fmt.Sprintf("%.4f°W", lon)
		},
		"fmttime": func(label string, t time.Time) string {
			return fmt.Sprintf("    %-6s %s", label+":", t.In(loc).Format(shortLayout))
		},
		"name": func(k int) string {
			day, _ := tithiName(k)
			return day
		},
		"paksha": func(k int) string {
			_, paksha := tithiName(k)
			return paksha
		},
		"current": func(iv Interval) bool {
			return !now.Before(iv.Start) && now.Before(iv.End)
		},
		"hl": func(current bool, block string) string {
			if !current || reverse == "" {
				return block
			}
			var (
				lines  = strings.Split(block, "\n")
				indent = len(lines[0]) - len(strings.TrimLeft(lines[0], " "))
				width  int
			)
			for _, l := range lines {
				width = max(width, utf8.RuneCountInString(l))
			}
			for i, l := range lines {
				pad := strings.Repeat(" ", width-utf8.RuneCountInString(l))
				lines[i] = l[:indent] + reverse + l[indent:] + pad + reset
			}
			return strings.Join(lines, "\n")
		},
	}).Parse(tmpl))

	render := func(name string) string {
		var sb strings.Builder
		if err := tpl.ExecuteTemplate(&sb, name, data); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return sb.String()
	}

	// Moon picture: the lunar day running now, else the first one shown.
	moonDay := 0
	for _, iv := range lunarDays {
		if moonDay == 0 || (!now.Before(iv.Start) && now.Before(iv.End)) {
			moonDay = iv.Number
		}
	}

	box := lipgloss.NewStyle().Padding(0, 2)
	left := lipgloss.JoinVertical(lipgloss.Left,
		box.Render(render("lunar")),
		"",
		box.Render(render("tithis")))
	right := box.Render(asciiMoon(moonDay))

	fmt.Println(render("header"))
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Center, left, right))
}
