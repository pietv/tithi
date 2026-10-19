package main

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
// This is the input for tithi arithmetic: tithi index = floor(elongation / 12).
// A waxing crescent reads near 0° and a full Moon near 180°.
//
// Both longitudes are referred to the true equinox of date: the Sun's via
// solar.ApparentLongitude (which includes nutation and aberration), the
// Moon's by adding nutation in longitude to moonposition.Position's mean-
// equinox value. The Moon's light-time aberration (~0.7″) is neglected; it
// would shift a tithi boundary by less than 8 seconds.
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
// rise.Times and rise.ApproxTimes work in the equatorial frame, while
// moonposition.Position yields ecliptic coordinates, so they are rotated
// here using the true obliquity (mean obliquity plus nutation in obliquity).
//
// Nutation in longitude is deliberately not applied: using the mean-equinox λ
// with the true obliquity stays within ~1″ of the fully apparent position,
// well within the ≈1 min accuracy achievable for rise/set times.
func moonEquatorial(jd float64) (unit.RA, unit.Angle, float64) {
	jde := jdToJDE(jd)

	λ, β, Δ := moonposition.Position(jde)
	_, Δε := nutation.Nutation(jde)
	ε := nutation.MeanObliquity(jde) + Δε
	sε, cε := ε.Sincos()
	α, δ := coord.EclToEq(λ, β, sε, cε)
	return α, δ, Δ
}

// moonrise computes the moonrise time for the UTC calendar date of probe
// (which must be a UTC midnight), for an observer at lat/lon (degrees,
// +N/+E). Returns time in UTC. Returns an error (e.g. rise.ErrorCircumpolar,
// rise.ErrorNeverRises, ErrNoConvergence) when the moon does not cross the
// effective horizon on that date at that latitude.
//
// The algorithm is Meeus Ch. 15: the crude estimate of rise.ApproxTimes,
// refined by the altitude correction (15.4a/15.4b) run to convergence on
// the true ephemeris. The effective horizon h0 accounts for the Moon's
// parallax, its disk and atmospheric refraction.
//
// # Why not rise.Times
//
// rise.Times applies the correction exactly once. For the fast Moon a single
// linear step consistently undershoots, leaving a residual of minutes, so the
// correction is repeated until the step is under 0.5s. If it is still larger
// after 10 rounds, the correction is oscillating rather than settling (at
// high latitudes the Moon can merely graze the horizon), and ErrNoConvergence
// is returned instead of a fabricated time.
//
// rise.Times also interpolates α and δ over the day before/of/after, and α
// wraps at 0h/24h, which corrupts the fit unless the samples are unwrapped.
// moonEquatorial can be evaluated at any instant, so each iteration just
// computes the true position instead.
//
// Note: the meeus library uses west-positive longitude, so lon is negated.
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
