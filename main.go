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
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"tithi/tzgeo"

	"github.com/soniakeys/meeus/v3/base"
	"github.com/soniakeys/meeus/v3/coord"
	"github.com/soniakeys/meeus/v3/deltat"
	"github.com/soniakeys/meeus/v3/globe"
	"github.com/soniakeys/meeus/v3/interp"
	"github.com/soniakeys/meeus/v3/julian"
	"github.com/soniakeys/meeus/v3/moonphase"
	"github.com/soniakeys/meeus/v3/moonposition"
	"github.com/soniakeys/meeus/v3/nutation"
	"github.com/soniakeys/meeus/v3/rise"
	"github.com/soniakeys/meeus/v3/sidereal"
	"github.com/soniakeys/meeus/v3/solar"
	"github.com/soniakeys/unit"
)

const (
	dateLayout  = "2006 Jan 2"
	shortLayout = "Jan 2 15:04 MST"

	inputLayout1 = "2006 Jan 2"
	inputLayout2 = "Jan 2"
	inputLayout3 = "2 Jan"
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
		// Lunar days 1..15
		"Pratipadā", "Dvitīyā", "Tritīyā", "Caturthī",
		"Pañcamī", "Ṣaṣṭhī", "Saptamī", "Aṣṭamī",
		"Navamī", "Daśamī", "Ekādaśī", "Dvādaśī",
		"Trayodaśī", "Caturdaśī", "Pūrṇimā",

		// Lunar days 16..30.
		"Pratipadā", "Dvitīyā", "Tritīyā", "Caturthī",
		"Pañcamī", "Ṣaṣṭhī", "Saptamī", "Aṣṭamī",
		"Navamī", "Daśamī", "Ekādaśī", "Dvādaśī",
		"Trayodaśī", "Caturdaśī", "Amāvāsyā",
	}[k-1], paksha
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
	jd := julian.TimeToJD(t.UTC())
	// Convert UT → TT for use as JDE.  ΔT grows slowly; yearly precision
	// is sufficient (≪1s impact on tithi boundary times).
	y, m, _ := t.Date()
	yearFrac := float64(y) + (float64(m)-0.5)/12
	dT := deltat.PolyAfter2000(yearFrac).Sec()
	jde := jd + dT/86400

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
	y, m, _ := julian.JDToCalendar(jd)
	yearFrac := float64(y) + (float64(m)-0.5)/12
	dT := deltat.PolyAfter2000(yearFrac).Sec()
	jde := jd + dT/86400

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
// The algorithm is Meeus Ch. 15, implemented by rise.Times.
//
// # Julian Day (jd)
//
// julian.CalendarGregorianToJD converts a Gregorian calendar date to a
// Julian Day Number — a continuous count of solar days since noon on
// 1 January 4713 BCE. All astronomical series in the ‘meeus’ package accept
// JD as the time argument. The day component is passed as a float64; an
// integer value (e.g. 15.0) corresponds to noon on that date, so the
// fractional part encodes the time of day. We pass the integer calendar
// day, giving the JD at noon; midnight of that date is jd − 0.5.
//
// # Three-day Moon ephemeris (α3, δ3)
//
// rise.Times uses a three-point Lagrange interpolation to find the precise
// instant the moon's altitude equals h0. It needs the moon's equatorial
// coordinates (α, δ) on the day before, the day of, and the day after the
// date of interest:
//
//	jd − 1 : previous day (interpolation left endpoint)
//	jd     : central day  (also supplies the distance Δ for parallax)
//	jd + 1 : following day (interpolation right endpoint)
//
// Three days are necessary because the Moon moves ≈12° in right ascension
// per day, and rise/set events near midnight need the adjacent days as
// interpolation end-points to stay accurate.
//
// # Observer position (p)
//
// globe.Coord holds the observer's geographic latitude and longitude.
// The meeus library follows the historical astronomical convention of
// west-positive longitude, so the east-positive lon argument is negated.
//
// # ΔT
//
// rise.Times applies ΔT internally when it evaluates the interpolated moon
// position at a candidate event time (converting the UT fraction-of-day to
// TT before calling the ephemeris). It therefore needs ΔT as a unit.Time;
// calling .Sec() on the returned value gives ΔT in seconds.
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
// # rise.ApproxTimes return values
//
// rise.ApproxTimes returns (tRise, tTransit, tSet, error); only tRise is
// used.  It is a unit.Time value representing elapsed time since 0h UT of
// the probe date.  Calling .Sec() converts a unit.Time to seconds.
// Multiplying by float64(time.Second) (nanoseconds per second) and wrapping
// in time.Duration gives the offset from the UTC midnight in probe,
// producing an absolute UTC timestamp for moonrise.
//
// # Why not rise.Times directly
//
// rise.Times (Meeus Ch. 15) applies its altitude correction (15.4a/15.4b)
// exactly once: it takes the crude ApproxTimes estimate, evaluates how far
// off the interpolated altitude is from h0 at that estimate, and steps by
// that single correction. For slow-moving objects (stars, planets, the Sun)
// one step lands within a few seconds. The Moon moves ≈12°/day in RA — far
// faster — so the local hour-angle rate sH·cδ·cLat used to convert an
// altitude error into a time offset is itself changing quickly near the
// estimate, and a single linear step consistently undershoots, leaving a
// residual on the order of minutes. Meeus notes this explicitly and says
// to repeat the correction "if sufficient accuracy is not attained"
// (Ch. 15, p.103). We do that here: same (15.4a/15.4b) correction step, run
// to convergence (<0.5s change) instead of applied once.
func moonrise(probe time.Time, lat, lon float64) (mr time.Time, err error) {
	py, pm, pd := probe.Date()
	jd := julian.CalendarGregorianToJD(py, int(pm), float64(pd))

	α3 := make([]unit.RA, 3)
	δ3 := make([]unit.Angle, 3)

	var Δ float64
	α3[0], δ3[0], _ = moonEquatorial(jd - 1)
	α3[1], δ3[1], Δ = moonEquatorial(jd)
	α3[2], δ3[2], _ = moonEquatorial(jd + 1)

	p := globe.Coord{
		Lat: unit.AngleFromDeg(lat),
		// Globe uses the west-positive convention.
		Lon: unit.AngleFromDeg(-lon),
	}
	yearFrac := float64(py) + (float64(pm)-0.5)/12
	ΔT := deltat.PolyAfter2000(yearFrac)
	Th0 := sidereal.Apparent0UT(jd)
	h0 := rise.Stdh0Lunar(moonposition.Parallax(Δ))

	tRise, _, _, e := rise.ApproxTimes(p, h0, Th0, α3[1], δ3[1])
	if e != nil {
		err = e
		return
	}

	αf := make([]float64, 3)
	δf := make([]float64, 3)
	for i := range α3 {
		αf[i] = α3[i].Rad()
		δf[i] = δ3[i].Rad()
	}
	d3α, e := interp.NewLen3(-86400, 86400, αf)
	if e != nil {
		err = e
		return
	}
	d3δ, e := interp.NewLen3(-86400, 86400, δf)
	if e != nil {
		err = e
		return
	}

	sLat, cLat := p.Lat.Sincos()
	// Same correction as rise.Times' internal adjustRS (Meeus 15.4a/15.4b),
	// but iterated to convergence rather than applied once.
	converge := func(m unit.Time) unit.Time {
		for range 10 {
			th0 := (Th0 + m.Mul(360.985647/360)).Mod1()
			ut := (m + ΔT).Sec()
			α := d3α.InterpolateX(ut)
			δ := d3δ.InterpolateX(ut)
			Hrad := th0.Rad() - p.Lon.Rad() - α
			sδ, cδ := math.Sincos(δ)
			sH, cH := math.Sincos(Hrad)
			h := math.Asin(sLat*sδ + cLat*cδ*cH)
			md := (unit.TimeFromRad(h) - h0.Time()).Div(cδ * cLat * sH)
			m += md
			if math.Abs(md.Sec()) < 0.5 {
				break
			}
		}
		return m
	}
	tRise = converge(tRise)

	mr = probe.Add(time.Duration(tRise.Sec() * float64(time.Second)))
	return
}

// Interval is one numbered lunar interval for a lunar day or a tithi.
type Interval struct {
	Number     int
	Start, End time.Time
}

// lunarSessions returns the lunar days and tithis overlapping the local
// solar day [dayStart, dayStart+24h) in chronological order, for an
// observer at lat/lon (degrees, +N/+E). dayStart must be a local midnight.
//
// Both kinds of intervals are anchored at the last new moon before dayStart:
// a new moon starts lunar day 1 and tithi 1, so walking boundary to
//
// The walk extends 48 hours past today's end — longer than any boundary spacing
// (a tithi lasts ≤ 27h, a lunar day ≤ ~26h) — so the intervals still in progress
// at day's end are closed by their real end boundary.
func lunarSessions(dayStart time.Time, lat, lon float64) (lunarDays, tithis []Interval) {
	var (
		dayEnd = dayStart.Add(24 * time.Hour)
		limit  = dayEnd.Add(48 * time.Hour)
		anchor = newMoonBefore(dayStart)

		overlapsToday = func(start, end time.Time) bool {
			return start.Before(dayEnd) && end.After(dayStart)
		}
	)

	// Lunar days. Gather every boundary in [anchor, limit): the new moons
	// and the moonrises. lunarRiseSet is probed once per UTC calendar day
	// and can report the same moonrise from two adjacent probes (see BUGS),
	// so a moonrise within an hour of the previously kept one is dropped.
	type boundary struct {
		at      time.Time
		newMoon bool
	}
	bounds := []boundary{{anchor, true}}
	for nm := anchor; nm.Before(limit); {
		nm = newMoonAfter(nm)
		bounds = append(bounds, boundary{nm, true})
	}
	var rises []time.Time
	for probe := startOfDay(anchor, time.UTC); probe.Before(limit); probe = probe.AddDate(0, 0, 1) {
		mr, err := moonrise(probe, lat, lon)
		if err == nil && mr.After(anchor) && mr.Before(limit) {
			rises = append(rises, mr)
		}
	}
	sort.Slice(rises, func(i, j int) bool { return rises[i].Before(rises[j]) })
	var lastRise time.Time
	for _, mr := range rises {
		if !lastRise.IsZero() && mr.Sub(lastRise) < time.Hour {
			continue
		}
		lastRise = mr
		bounds = append(bounds, boundary{mr, false})
	}
	sort.Slice(bounds, func(i, j int) bool { return bounds[i].at.Before(bounds[j].at) })

	// Successive boundaries delimit the lunar days; numbering restarts at 1
	// on each new moon.
	number := 0
	for i := 0; i+1 < len(bounds); i++ {
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

// decimalYear converts t to the fractional-year representation expected by
// moonphase.* (e.g. 2026.29 for ~mid-April 2026).  Precision of ±1 day is
// sufficient, since moonphase only uses it to select the lunation number k.
func decimalYear(t time.Time) float64 {
	return float64(t.Year()) + (float64(t.YearDay())-1)/365.25
}

// newMoonJDEtoUT converts a moonphase JDE result (TT-based) to a UTC
// time.Time, subtracting ΔT for the approximate year.
func newMoonJDEtoUT(jde, approxYear float64) time.Time {
	dT := deltat.PolyAfter2000(approxYear).Sec()
	return julian.JDToTime(jde - dT/86400)
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
// elongation crossing).  A tithi lasts 20–27 hours, so the next boundary
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
// *time.Location. Accepts a fixed offset ("+05:30", "-08:00", "Z"),
// or empty for system timezone.
func parseLocation(tz string) (*time.Location, error) {
	if tz == "" {
		return time.Local, nil
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc, nil
	}
	if t, err := time.Parse("Z07:00", tz); err == nil {
		_, off := t.Zone()
		return time.FixedZone(tz, off), nil
	}
	return nil, fmt.Errorf("cannot parse timezone %q", tz)
}

func main() {
	latFlag := flag.Float64("lat", 0, "latitude in degrees (+N); default: the current timezone location")
	lonFlag := flag.Float64("lon", 0, "longitude in degrees (+E); default: the current timezone location")
	tzFlag := flag.String("tz", "", fmt.Sprintf("timezone: IANA name, ±HH:MM, or empty for system (TZ=%q)", os.Getenv("TZ")))
	dateFlag := flag.String("date", "", "date other than today (in format: “2026 Jan 01”, “Jan 01”, or “1 Jan”)")
	flag.Usage = func() {
		program := filepath.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage: %s [-tz <zone>] [-lat <deg>] [-lon <deg>] [-date DATE]\n", program)
		flag.PrintDefaults()
	}
	flag.Parse()

	loc, err := parseLocation(*tzFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// latSet/lonSet track whether the user explicitly passed -lat/-lon, as
	// opposed to leaving them at their zero-value default. This lets 0°
	// latitude/longitude be honored, and keeps the coordinates-derived-from-tz
	// label from being attached when the user supplied their own coordinates.
	var latSet, lonSet bool
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "lat":
			latSet = true
		case "lon":
			lonSet = true
		}
	})

	// Resolve an IANA zone name suitable for tzgeo lookup.
	zoneName := loc.String()
	if *tzFlag != "" {
		zoneName = *tzFlag
	}
	var ianaName string
	zoneMatched := false
	if zLat, zLon, ok := tzgeo.Lookup(zoneName); ok {
		zoneMatched = true
		if !latSet {
			*latFlag = zLat
		}
		if !lonSet {
			*lonFlag = zLon
		}
		if !latSet && !lonSet {
			ianaName = zoneName
		}
	}

	// Fallback: explicit $TZ env var, then hardcoded default.
	for _, name := range []string{os.Getenv("TZ"), "Europe/Moscow"} {
		if name == "" || latSet || lonSet || zoneMatched {
			break
		}
		if zLat, zLon, ok := tzgeo.Lookup(name); ok {
			*latFlag, *lonFlag = zLat, zLon
			if ianaName == "" {
				ianaName = name
			}
			zoneMatched = true
		}
	}
	if !latSet && !lonSet && (*latFlag == 0 || *lonFlag == 0) {
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
			os.Exit(2)
		}

	}

	var (
		fmtlat = func(lat float64) string {
			if lat >= 0 {
				return fmt.Sprintf("%.4f°N", lat)
			}
			return fmt.Sprintf("%.4f°S", -lat)
		}
		fmtlon = func(lon float64) string {
			if lon >= 0 {
				return fmt.Sprintf("%.4f°E", lon)
			}
			return fmt.Sprintf("%.4f°W", -lon)
		}
		place = fmt.Sprintf("-lat=%s -lon=%s", fmtlat(*latFlag), fmtlon(*lonFlag))
	)
	if ianaName != "" {
		place += fmt.Sprintf(" -tz=%q", ianaName)
	}
	fmt.Printf("Location: %s\n\n", place)

	dayStart := startOfDay(date.In(loc), loc)
	lunarDays, tithis := lunarSessions(dayStart, *latFlag, *lonFlag)

	fmt.Printf("%s:", dayStart.Format(dateLayout))
	fmtBoundary := func(label string, t time.Time) {
		fmt.Printf("      %-10s %s\n", label+":", t.In(loc).Format(shortLayout))
	}
	for _, ld := range lunarDays {
		fmt.Printf("\n  %d Lunar Day:\n", ld.Number)
		fmtBoundary("start", ld.Start)
		fmtBoundary("end", ld.End)
	}
	fmt.Println()

	fmt.Println("Tithis:")
	for _, t := range tithis {
		name, paksha := tithiName(t.Number)
		fmt.Printf("    %d %s (%s):\n", t.Number, name, paksha)
		fmtBoundary("start", t.Start)
		fmtBoundary("end", t.End)
		fmt.Println()
	}
}
