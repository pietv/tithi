// Package tithi computes lunar days and tithis for a given date and
// geographical location.
//
// A lunar day is the interval between a new moon and the next moonrise,
// or between two consecutive moonrises, or between the last moonrise
// and the new moon. There are 29 or 30 lunar days between two new moons.
//
// A tithi is the interval during which the elongation of the Moon from
// the Sun increases by 12°. There are exactly 30 tithis in a synodic
// (lunar) month, numbered 1..15 in the waxing fortnight (Śukla pakṣa)
// and 16..30 in the waning fortnight (Kṛṣṇa pakṣa).
//
// [Calendar] is the main entry point. Latitudes and longitudes are in
// degrees, positive for north and east, negative for south and west.
package tithi

import (
	"errors"
	"math"
	"sort"
	"time"

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
)

// Name returns a tithi day name and the period (pakṣa) for a tithi
// number (1..30), where 1 is the first tithi of Śukla pakṣa.
func Name(k int) (day, paksha string) {
	if k < 1 || k > 30 {
		panic("k must be between 1 and 30")
	}
	paksha = "Śukla pakṣa"
	if k > 15 {
		paksha = "Kṛṣṇa pakṣa"
	}
	return [...]string{
		// Waxing fortnight tithi 1..15.
		"Pratipadā", "Dvitīyā", "Tṛtīyā", "Caturthī",
		"Pañcamī", "Ṣaṣṭhī", "Saptamī", "Aṣṭamī",
		"Navamī", "Daśamī", "Ekādaśī", "Dvādaśī",
		"Trayodaśī", "Caturdaśī", "Pūrṇimā",

		// Waning fortnight tithi 16..30.
		"Pratipadā", "Dvitīyā", "Tṛtīyā", "Caturthī",
		"Pañcamī", "Ṣaṣṭhī", "Saptamī", "Aṣṭamī",
		"Navamī", "Daśamī", "Ekādaśī", "Dvādaśī",
		"Trayodaśī", "Caturdaśī", "Amāvāsyā",
	}[k-1], paksha
}

// Index returns the number of the tithi (1..30) running at time t, as
// accepted by [Name] and reported in [Interval.Index]: 1 right after a
// new moon, 15 ending at the full moon, 30 just before the next new moon.
func Index(t time.Time) int { return int(math.Floor(MoonElongation(t)/12)) + 1 }

// jdToJDE converts a UT-based Julian Day to a Julian Ephemeris Day (TT)
// by adding ΔT. Yearly-granularity ΔT is sufficient for all ephemeris
// computations in this program (≪1s impact on tithi boundary times).
func jdToJDE(jd float64) float64 {
	y, m, _ := julian.JDToCalendar(jd)
	yearFrac := float64(y) + (float64(m)-0.5)/12
	dT := deltat.PolyAfter2000(yearFrac).Sec()
	return jd + dT/86400
}

// MoonElongation returns the Moon's elongation from the Sun,
// (λMoon − λSun) mod 360, in degrees, measured eastward.
//
// MoonElongation is the input for tithi arithmetic:
//
//	tithi index = floor(elongation / 12) + 1
//
// Both longitudes are referred to the true equinox of date: the Sun's via
// [solar.ApparentLongitude] (which includes nutation and aberration), the
// Moon's by adding nutation in longitude to [moonposition.Position]'s mean
// equinox value. The Moon's light-time aberration (~0.7″) is neglected; it
// would shift a tithi boundary by less than 8 seconds.
func MoonElongation(t time.Time) float64 {
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
// [rise.Times] and [rise.ApproxTimes] work in the equatorial frame, while
// [moonposition.Position] yields ecliptic coordinates, so they are rotated
// here using the true obliquity (mean obliquity plus nutation in obliquity).
//
// Nutation in longitude is deliberately not applied: using the mean-equinox λ
// with the true obliquity stays within ≲17″ of the fully apparent position
// (~1 s of rise time), well within the ≈1 min accuracy achievable for
// rise/set times.
func moonEquatorial(jd float64) (unit.RA, unit.Angle, float64) {
	jde := jdToJDE(jd)

	λ, β, Δ := moonposition.Position(jde)
	_, Δε := nutation.Nutation(jde)
	ε := nutation.MeanObliquity(jde) + Δε
	sε, cε := ε.Sincos()
	α, δ := coord.EclToEq(λ, β, sε, cε)
	return α, δ, Δ
}

// ErrNoConvergence is returned by [Moonrise] when the iterated altitude
// correction oscillates instead of settling, meaning the Moon only grazes
// the effective horizon on the probed date and no reliable rise time exists.
var ErrNoConvergence = errors.New("altitude correction did not converge")

// Moonrise computes the moonrise time for the UTC calendar date of day,
// for an observer at lat/lon (degrees, +N/+E). Returns time in UTC.
// Returns an error (e.g. [rise.ErrorCircumpolar], [rise.ErrorNeverRises],
// [ErrNoConvergence]) when the moon does not cross the effective horizon
// on that date at that latitude.
//
// The algorithm is Meeus Ch. 15: the crude estimate of [rise.ApproxTimes],
// refined by the altitude correction (15.4a/15.4b) run to convergence on
// the true ephemeris. The effective horizon h0 accounts for the Moon's
// parallax, its disk and atmospheric refraction.
//
// Why not [rise.Times]? [rise.Times] applies the correction exactly once.
// For the fast Moon a single linear step consistently undershoots, leaving
// a residual of minutes, so the correction is repeated until the step
// is under 0.5s. If it is still larger after 10 rounds, the correction is
// oscillating rather than settling (at high latitudes the Moon can merely
// graze the horizon), and [ErrNoConvergence] is returned.
func Moonrise(day time.Time, lat, lon float64) (time.Time, error) {
	py, pm, pd := day.UTC().Date()
	probe := time.Date(py, pm, pd, 0, 0, 0, 0, time.UTC)

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

// Interval is a lunar day or a tithi: its index (1..30) and the time span
// it covers. For lunar days the index restarts at each new moon; for tithis
// it is the index accepted by [Name].
type Interval struct {
	Index      int
	Start, End time.Time
}

// Calendar returns the lunar days and tithis overlapping the civil day
// in chronological order for an observer at lat/lon (degrees, +N/+E).
// The civil day is the one containing day in day's own location
// (use [time.Time.In] to pick another zone); its time of day is ignored.
//
// Both kinds of periods are anchored at the last new moon before the day:
// a new moon starts lunar day 1 and tithi 1, so walking boundary to boundary
// from the anchor indexes every period along the way.
//
// The walk extends 48 hours past day's end — longer than any boundary spacing
// (a tithi lasts ≤ 27h, a lunar day ≤ ~26h) — so the periods still in progress
// at day's end are closed by their real end boundary.
func Calendar(day time.Time, lat, lon float64) (lunarDays []Interval, tithis []Interval) {
	var (
		loc      = day.Location()
		y, m, d  = day.Date()
		dayStart = time.Date(y, m, d, 0, 0, 0, 0, loc)
		dayEnd   = time.Date(y, m, d+1, 0, 0, 0, 0, loc)
		limit    = dayEnd.Add(48 * time.Hour)
		anchor   = PrevNewMoon(dayStart)
	)

	// Lunar days. Gather every boundary in [anchor, limit): the new moons
	// and the moonrises. Moonrise is probed once per UTC calendar day, so
	// the rises come out in order (real moonrises are ≈25h apart) except
	// that two adjacent probes can report the same moonrise twice;
	// a rise within an hour of the previously kept one is dropped.
	type boundary struct {
		at      time.Time
		newMoon bool
	}
	bounds := []boundary{{anchor, true}}
	for nm := anchor; nm.Before(limit); {
		nm = NextNewMoon(nm)
		bounds = append(bounds, boundary{nm, true})
	}
	var (
		lastRise   time.Time
		ay, am, ad = anchor.UTC().Date()
	)
	for probe := time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC); probe.Before(limit); probe = probe.AddDate(0, 0, 1) {
		mr, err := Moonrise(probe, lat, lon)
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

	// Successive boundaries delimit the lunar days; the index restarts at 1
	// on each new moon.
	for index, i := 0, 0; i+1 < len(bounds); i++ {
		if bounds[i].newMoon {
			index = 1
		} else {
			index++
		}
		if overlapsToday(bounds[i].at, bounds[i+1].at) {
			lunarDays = append(lunarDays, Interval{index, bounds[i].at, bounds[i+1].at})
		}
	}

	// Tithis. The anchor new moon is the 0° elongation crossing, but only
	// to within a few minutes, so the walk starts from the exact crossing:
	// the first boundary after a point safely before the anchor. Successive
	// boundaries then delimit the tithis. Each is indexed from the
	// elongation at its midpoint, far from any numerical jitter at the
	// boundaries.
	for start := NextBoundary(anchor.Add(-30 * time.Minute)); start.Before(dayEnd); {
		end := NextBoundary(start)
		if overlapsToday(start, end) {
			tithis = append(tithis, Interval{Index(start.Add(end.Sub(start) / 2)), start, end})
		}
		start = end
	}
	return lunarDays, tithis
}

// newMoonJDEtoUT converts a moonphase JDE result (TT-based) to a UTC
// time.Time, subtracting ΔT for the approximate year.
func newMoonJDEtoUT(jde, approxYear float64) time.Time {
	dT := deltat.PolyAfter2000(approxYear).Sec()
	return julian.JDToTime(jde - dT/86400)
}

// decimalYear converts t to the fractional-year representation expected by
// [moonphase.New] (e.g. 2026.29 for mid-April 2026).
func decimalYear(t time.Time) float64 {
	return float64(t.Year()) + (float64(t.YearDay())-1)/365.25
}

// PrevNewMoon returns the latest new moon strictly before t
// (seeded 14 days back to skip float-noise duplicates).
func PrevNewMoon(t time.Time) time.Time {
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

// NextNewMoon returns the earliest new moon strictly after t.
func NextNewMoon(t time.Time) time.Time {
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

// NextBoundary returns the first instant after t at which the tithi changes,
// to within a second. A tithi lasts 18–26 hours, so the boundary lies inside
// (t, t+30h); bisection over that bracket finds it, because [Index] changes
// exactly once there.
func NextBoundary(t time.Time) time.Time {
	lo, hi := t, t.Add(30*time.Hour)
	k := Index(lo)
	for hi.Sub(lo) > time.Second {
		mid := lo.Add(hi.Sub(lo) / 2)
		if Index(mid) == k {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi
}
