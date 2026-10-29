package tithi

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pietv/tzgeo"
)

// Reference data: new moons (2026-Oct-10, 2021-Oct-6, 2000-Oct-27) and
// full moons (2026-Oct-26, 2021-Oct-20, 2000-Oct-13) are the published
// lunation times (USNO / timeanddate.com), checked against the tithi 30→1
// and 15→16 shifts.
var CalendarTests = []struct {
	tz, date     string
	lunar, tithi string
}{
	// Lat: 52.5, Lon: 13.3667.
	{tz: "Europe/Berlin", date: "2026-Oct-5", lunar: "24, 25", tithi: "24, 25, 26"},
	// Lat: 40.7142, Lon: -74.0064.
	{tz: "America/New_York", date: "2026-Oct-5", lunar: "24, 25", tithi: "25, 26"},
	// Lat: -33.8667, Lon: 151.2167.
	{tz: "Australia/Sydney", date: "2026-Oct-5", lunar: "23, 24", tithi: "24, 25"},
	// Lat: 35.6544, Lon: 139.7447.
	{tz: "Asia/Tokyo", date: "2026-Oct-10", lunar: "28, 29", tithi: "29, 30"},
	// Lat: 41.85, Lon: -87.65.
	{tz: "America/Chicago", date: "2026-Oct-10", lunar: "29, 30, 1", tithi: "30, 1"},
	// Lat: -34.6, Lon: -58.45.
	{tz: "America/Argentina/Buenos_Aires", date: "2026-Oct-10", lunar: "29, 30, 1", tithi: "30, 1"},
	// Lat: 22.5333, Lon: 88.3667.
	{tz: "Asia/Kolkata", date: "2026-Oct-17", lunar: "7, 8", tithi: "6, 7"},
	// Lat: 34.0522, Lon: -118.2428.
	{tz: "America/Los_Angeles", date: "2026-Oct-17", lunar: "7, 8", tithi: "7, 8"},
	// Lat: -12.05, Lon: -77.05.
	{tz: "America/Lima", date: "2026-Oct-17", lunar: "7, 8", tithi: "7, 8"},
	// Lat: 55.7558, Lon: 37.6178.
	{tz: "Europe/Moscow", date: "2026-Oct-26", lunar: "16, 17", tithi: "15, 16"},
	// Lat: 19.4, Lon: -99.15.
	{tz: "America/Mexico_City", date: "2026-Oct-26", lunar: "16, 17", tithi: "16, 17"},
	// Lat: -33.45, Lon: -70.6667.
	{tz: "America/Santiago", date: "2026-Oct-26", lunar: "16, 17", tithi: "15, 16, 17"},

	// Lat: 52.5, Lon: 13.3667.
	{tz: "Europe/Berlin", date: "2021-Oct-6", lunar: "29, 30, 1", tithi: "30, 1"},
	// Lat: 40.7142, Lon: -74.0064.
	{tz: "America/New_York", date: "2021-Oct-6", lunar: "29, 30, 1", tithi: "30, 1"},
	// Lat: -33.8667, Lon: 151.2167.
	{tz: "Australia/Sydney", date: "2021-Oct-6", lunar: "28, 29, 1", tithi: "29, 30, 1"},
	// Lat: 41.85, Lon: -87.65.
	{tz: "America/Chicago", date: "2021-Oct-10", lunar: "5, 6", tithi: "5, 6"},
	// Lat: -26.25, Lon: 28.0.
	{tz: "Africa/Johannesburg", date: "2021-Oct-10", lunar: "4, 5", tithi: "4, 5, 6"},
	// Lat: -34.6, Lon: -58.45.
	{tz: "America/Argentina/Buenos_Aires", date: "2021-Oct-10", lunar: "4, 5", tithi: "5, 6"},
	// Lat: 22.5333, Lon: 88.3667.
	{tz: "Asia/Kolkata", date: "2021-Oct-20", lunar: "14, 15", tithi: "15, 16"},
	// Lat: 34.0522, Lon: -118.2428.
	{tz: "America/Los_Angeles", date: "2021-Oct-20", lunar: "15, 16", tithi: "15, 16"},
	// Lat: 55.7558, Lon: 37.6178.
	{tz: "Europe/Moscow", date: "2021-Oct-29", lunar: "23, 24", tithi: "23, 24"},
	// Lat: 19.4, Lon: -99.15.
	{tz: "America/Mexico_City", date: "2021-Oct-29", lunar: "23, 24", tithi: "23, 24"},
	// Lat: -31.95, Lon: 115.85.
	{tz: "Australia/Perth", date: "2021-Oct-29", lunar: "22, 23", tithi: "23, 24"},
	// Lat: -33.45, Lon: -70.6667.
	{tz: "America/Santiago", date: "2021-Oct-29", lunar: "22, 23", tithi: "23, 24"},

	// Lat: 52.5, Lon: 13.3667.
	{tz: "Europe/Berlin", date: "2000-Oct-5", lunar: "8, 9", tithi: "8"},
	// Lat: 40.7142, Lon: -74.0064.
	{tz: "America/New_York", date: "2000-Oct-5", lunar: "8, 9", tithi: "8, 9"},
	// Lat: -33.8667, Lon: 151.2167.
	{tz: "Australia/Sydney", date: "2000-Oct-5", lunar: "8, 9", tithi: "7, 8"},
	// Lat: 35.6544, Lon: 139.7447.
	{tz: "Asia/Tokyo", date: "2000-Oct-13", lunar: "16, 17", tithi: "15, 16"},
	// Lat: 22.5333, Lon: 88.3667.
	{tz: "Asia/Kolkata", date: "2000-Oct-21", lunar: "24", tithi: "23, 24, 25"},
	// Lat: 34.0522, Lon: -118.2428.
	{tz: "America/Los_Angeles", date: "2000-Oct-21", lunar: "23, 24", tithi: "24, 25"},
	// Lat: -6.1667, Lon: 106.8.
	{tz: "Asia/Jakarta", date: "2000-Oct-21", lunar: "23, 24", tithi: "23, 24"},
	// Lat: -12.05, Lon: -77.05.
	{tz: "America/Lima", date: "2000-Oct-21", lunar: "23, 24", tithi: "24, 25"},
	// Lat: 55.7558, Lon: 37.6178.
	{tz: "Europe/Moscow", date: "2000-Oct-27", lunar: "29, 30, 1", tithi: "30, 1"},
	// Lat: 19.4, Lon: -99.15.
	{tz: "America/Mexico_City", date: "2000-Oct-27", lunar: "29, 1, 2", tithi: "30, 1"},
	// Lat: -31.95, Lon: 115.85.
	{tz: "Australia/Perth", date: "2000-Oct-27", lunar: "29, 30, 1", tithi: "30, 1"},
	// Lat: -33.45, Lon: -70.6667.
	{tz: "America/Santiago", date: "2000-Oct-27", lunar: "29, 1, 2", tithi: "30, 1"},
}

func TestCalendar(t *testing.T) {
	for _, tc := range CalendarTests {
		loc, err := time.LoadLocation(tc.tz)
		if err != nil {
			t.Fatal(err)
		}
		lat, lon, ok := tzgeo.Lookup(tc.tz)
		if !ok {
			t.Fatalf("no coordinates for %s", tc.tz)
		}
		day, err := time.ParseInLocation("2006-Jan-02", tc.date, loc)
		if err != nil {
			t.Fatal(err)
		}

		indexes := func(n int, number func(int) int) string {
			s := make([]string, n)
			for i := range s {
				s[i] = fmt.Sprint(number(i))
			}
			return strings.Join(s, ", ")
		}
		lunarDays, tithis := Calendar(day, lat, lon)
		if got := indexes(len(lunarDays), func(i int) int { return lunarDays[i].Index }); got != tc.lunar {
			t.Errorf("%s %s: lunar days = %q, want %q", tc.tz, tc.date, got, tc.lunar)
		}
		if got := indexes(len(tithis), func(i int) int { return tithis[i].Index }); got != tc.tithi {
			t.Errorf("%s %s: tithis = %q, want %q", tc.tz, tc.date, got, tc.tithi)
		}
	}
}

// NextBoundary must work from any instant, not only from a boundary:
// the tithi changes exactly there, and chaining visits every tithi in turn.
func TestNextBoundary(t *testing.T) {
	t0 := time.Date(2026, 10, 5, 13, 17, 0, 0, time.UTC)
	k := Index(t0)
	for range 40 {
		b := NextBoundary(t0)
		if !b.After(t0) || b.Sub(t0) > 30*time.Hour {
			t.Fatalf("NextBoundary(%v) = %v, not within (t, t+30h]", t0, b)
		}
		if got := Index(b.Add(-2 * time.Second)); got != k {
			t.Errorf("tithi 2s before boundary %v = %d, want %d", b, got, k)
		}
		k = k%30 + 1
		if got := Index(b); got != k {
			t.Errorf("tithi at boundary %v = %d, want %d", b, got, k)
		}
		t0 = b.Add(time.Minute)
	}
}
