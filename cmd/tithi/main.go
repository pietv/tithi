// Tithi prints the lunar days and tithis for a given date and geographical
// location (see the [tithi] package for what those are).
//
// [tithi]: https://pkg.go.dev/github.com/pietv/tithi
package main

import (
	"cmp"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pietv/tithi"
	"github.com/pietv/tzgeo"
)

// defaultTZ supplies the coordinates when neither -tz, $TZ, nor -lat/-lon
// say where the observer is.
const defaultTZ = "Europe/Moscow"

type location struct {
	loc      *time.Location
	lat, lon float64
	zone     string
}

// resolveLocation works out the observer's location. Coördinates default to those
// of the timezone (-tz, else $TZ, else defaultTZ); a nonzero -lat or -lon
// overrides them..
func resolveLocation(tz string, lat, lon float64) (location, error) {
	l := location{loc: time.Local}
	if tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return l, fmt.Errorf("cannot parse timezone %q", tz)
		}
		l.loc = loc
	}

	zone := cmp.Or(tz, os.Getenv("TZ"), defaultTZ)
	var known bool
	l.lat, l.lon, known = tzgeo.Lookup(zone)
	if !known && (lat == 0 || lon == 0) {
		return l, fmt.Errorf("no coordinates known for timezone %q: use -lat and -lon", zone)
	}
	switch {
	case lat == 0 && lon == 0:
		l.zone = zone
	case lat != 0:
		l.lat = lat
	case lon != 0:
		l.lon = lon
	default:
	}
	return l, nil
}

// parseDate parses s in loc as "2026 Jan 1", "2026-Jan-1", "Jan 1" or "1 Jan",
// supplying the current year (as of now) when s has none.
func parseDate(s string, loc *time.Location, now time.Time) (time.Time, error) {
	year := strconv.Itoa(now.In(loc).Year()) + " "
	for _, c := range []struct{ layout, input string }{
		{"2006 Jan 2", s},
		{"2006-Jan-2", s},
		{"2006 Jan 2", year + s},
		{"2006 2 Jan", year + s},
	} {
		if t, err := time.ParseInLocation(c.layout, c.input, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date %q", s)
}

func main() {
	latFlag := flag.Float64("lat", 0, "latitude in degrees (+N, -S); default: from the timezone")
	lonFlag := flag.Float64("lon", 0, "longitude in degrees (+E, -W); default: from the timezone")
	tzFlag := flag.String("tz", "", fmt.Sprintf("timezone: IANA name, or empty for system (TZ=%q)", os.Getenv("TZ")))
	dateFlag := flag.String("date", "", fmt.Sprintf("date other than today (%q, %q, %q, or %q)", "2026 Jan 01", "2026-Jan-01", "Jan 01", "1 Jan"))
	flag.Usage = func() {
		program := filepath.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage: %s [-tz <zone>] [-lat <deg>] [-lon <deg>] [-date DATE]\n", program)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "Print the lunar day for a specified date and location (print for today\n")
		fmt.Fprintf(os.Stderr, "and for the location set in the ‘TZ’ environment variable by default).\n")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, `  $ %s -date "Sep 12"    -tz="Africa/Johannesburg"`+"\n", program)
		fmt.Fprintf(os.Stderr, `  $ %s -date "12 Aug"    -tz="Australia/Melbourne"`+"\n", program)
		fmt.Fprintf(os.Stderr, "  $ %s -date 2026-Oct-12\n", program)
		fmt.Fprintf(os.Stderr, "  $ %s -lat=19.4 -lon=-99\n", program)
		fmt.Fprintln(os.Stderr)
	}
	flag.Parse()

	l, err := resolveLocation(*tzFlag, *latFlag, *lonFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	now := time.Now()
	date := now
	if *dateFlag != "" {
		if date, err = parseDate(*dateFlag, l.loc, now); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	day := date.In(l.loc)
	lunarDays, tithis := tithi.Calendar(day, l.lat, l.lon)

	data := struct {
		Lat, Lon  float64
		IANA      string
		Day       string
		LunarDays []tithi.Interval
		Tithis    []tithi.Interval
	}{
		Lat:       l.lat,
		Lon:       l.lon,
		IANA:      l.zone,
		Day:       day.Format(dateLayout),
		LunarDays: lunarDays,
		Tithis:    tithis,
	}

	tpl := newTemplate(l.loc, now)
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
