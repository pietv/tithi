// Tithi computes the current lunar day and tithi for a given
// geographical location and current tim e.
//
// A lunar day is the interval between a new moon and the next moon rise,
// or between two consecutive moon rises, or between the last moon rise
// and the new moon. There are 29 or 30 lunar days between two new moons.
//
// A tithi is the interval during which the elongation of the Moon from
// the Sun increases by 12°. There are exactly 30 tithis in a synodic
// (lunar) month, numbered 1..15 in the waxing fortnight (Śukla pakṣa)
// and 16..30 in the waning fortnight (Kṛṣṇa pakṣa).
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pietv/tithi/tzgeo"
)

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

const (
	inputLayout1 = "2006 Jan 2"
	inputLayout2 = "2006-Jan-2"
	inputLayout3 = "Jan 2"
	inputLayout4 = "2 Jan"
)

func main() {
	latFlag := flag.Float64("lat", math.NaN(), "latitude in degrees (+N, -S); default: current timezone")
	lonFlag := flag.Float64("lon", math.NaN(), "longitude in degrees (+E, -W); default: current timezone")
	tzFlag := flag.String("tz", "", fmt.Sprintf("timezone: IANA name, or empty for system (TZ=%q)", os.Getenv("TZ")))
	dateFlag := flag.String("date", "", fmt.Sprintf("date other than today (formatted as: %q, %q, %q, or %q)", "2026 Jan 01", "2026-Jan-01", "Jan 01", "1 Jan"))
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
		for _, layout := range []string{inputLayout1, inputLayout2, inputLayout3, inputLayout4} {
			date, err = time.ParseInLocation(layout, *dateFlag, loc)
			if err != nil {
				continue
			}
			matched = true

			// Supply the current year for parsed dates without a year.
			if date.Year() == 0 {
				date = date.AddDate(currentYear, 0, 0)
			}
			break
		}
		if !matched {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

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
	tpl := newTemplate(loc, now)
	render := func(name string) string {
		var sb strings.Builder
		if err := tpl.ExecuteTemplate(&sb, name, data); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return sb.String()
	}

	fmt.Println(render("header"))
	fmt.Println(layout(render("lunar"), render("tithis"), moonDay(lunarDays, now)))
}
