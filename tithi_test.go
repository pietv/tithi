package tithi

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pietv/tzgeo"
)

const dateLayout = "2006-Jan-2"

// Reference data: new moons (2026-Oct-10, 2021-Oct-6, 2000-Oct-27) and
// full moons (2026-Oct-26, 2021-Oct-20, 2000-Oct-13) are the published
// lunation times (USNO / timeanddate.com), checked against the tithi 30→1
// and 15→16 shifts.
var calendarTests = []struct {
	tz, date     string
	lunar, tithi string
}{
	{tz: "Europe/Berlin", date: "2026-Oct-5",
		lunar: "24, 25", tithi: "24, 25, 26"},
	{tz: "America/New_York", date: "2026-Oct-5",
		lunar: "24, 25", tithi: "25, 26"},
	{tz: "Australia/Sydney", date: "2026-Oct-5",
		lunar: "23, 24", tithi: "24, 25"},
	{tz: "America/Sao_Paulo", date: "2026-Oct-5",
		lunar: "24, 25", tithi: "25, 26"},
	{tz: "Asia/Tokyo", date: "2026-Oct-10",
		lunar: "28, 29", tithi: "29, 30"},
	{tz: "America/Chicago", date: "2026-Oct-10",
		lunar: "29, 30, 1", tithi: "30, 1"},
	{tz: "Africa/Johannesburg", date: "2026-Oct-10",
		lunar: "29, 30, 1", tithi: "30, 1"},
	{tz: "America/Argentina/Buenos_Aires", date: "2026-Oct-10",
		lunar: "29, 30, 1", tithi: "30, 1"},
	{tz: "Asia/Kolkata", date: "2026-Oct-17",
		lunar: "7, 8", tithi: "6, 7"},
	{tz: "America/Los_Angeles", date: "2026-Oct-17",
		lunar: "7, 8", tithi: "7, 8"},
	{tz: "Asia/Jakarta", date: "2026-Oct-17",
		lunar: "7, 8", tithi: "6, 7"},
	{tz: "America/Lima", date: "2026-Oct-17",
		lunar: "7, 8", tithi: "7, 8"},
	{tz: "Europe/Moscow", date: "2026-Oct-26",
		lunar: "16, 17", tithi: "15, 16"},
	{tz: "America/Mexico_City", date: "2026-Oct-26",
		lunar: "16, 17", tithi: "16, 17"},
	{tz: "Australia/Perth", date: "2026-Oct-26",
		lunar: "16, 17", tithi: "15, 16"},
	{tz: "America/Santiago", date: "2026-Oct-26",
		lunar: "16, 17", tithi: "15, 16, 17"},

	{tz: "Europe/Berlin", date: "2021-Oct-6",
		lunar: "29, 30, 1", tithi: "30, 1"},
	{tz: "America/New_York", date: "2021-Oct-6",
		lunar: "29, 30, 1", tithi: "30, 1"},
	{tz: "Australia/Sydney", date: "2021-Oct-6",
		lunar: "28, 29, 1", tithi: "29, 30, 1"},
	{tz: "America/Sao_Paulo", date: "2021-Oct-6",
		lunar: "29, 30, 1", tithi: "30, 1"},
	{tz: "Asia/Tokyo", date: "2021-Oct-10",
		lunar: "4, 5", tithi: "4, 5"},
	{tz: "America/Chicago", date: "2021-Oct-10",
		lunar: "5, 6", tithi: "5, 6"},
	{tz: "Africa/Johannesburg", date: "2021-Oct-10",
		lunar: "4, 5", tithi: "4, 5, 6"},
	{tz: "America/Argentina/Buenos_Aires", date: "2021-Oct-10",
		lunar: "4, 5", tithi: "5, 6"},
	{tz: "Asia/Kolkata", date: "2021-Oct-20",
		lunar: "14, 15", tithi: "15, 16"},
	{tz: "America/Los_Angeles", date: "2021-Oct-20",
		lunar: "15, 16", tithi: "15, 16"},
	{tz: "Asia/Jakarta", date: "2021-Oct-20",
		lunar: "14, 15", tithi: "15, 16"},
	{tz: "America/Lima", date: "2021-Oct-20",
		lunar: "14, 15", tithi: "15, 16"},
	{tz: "Europe/Moscow", date: "2021-Oct-29",
		lunar: "23, 24", tithi: "23, 24"},
	{tz: "America/Mexico_City", date: "2021-Oct-29",
		lunar: "23, 24", tithi: "23, 24"},
	{tz: "Australia/Perth", date: "2021-Oct-29",
		lunar: "22, 23", tithi: "23, 24"},
	{tz: "America/Santiago", date: "2021-Oct-29",
		lunar: "22, 23", tithi: "23, 24"},

	{tz: "Europe/Berlin", date: "2000-Oct-5",
		lunar: "8, 9", tithi: "8"},
	{tz: "America/New_York", date: "2000-Oct-5",
		lunar: "8, 9", tithi: "8, 9"},
	{tz: "Australia/Sydney", date: "2000-Oct-5",
		lunar: "8, 9", tithi: "7, 8"},
	{tz: "America/Sao_Paulo", date: "2000-Oct-5",
		lunar: "8, 9", tithi: "8, 9"},
	{tz: "Asia/Tokyo", date: "2000-Oct-13",
		lunar: "16, 17", tithi: "15, 16"},
	{tz: "America/Chicago", date: "2000-Oct-13",
		lunar: "16, 17", tithi: "15, 16"},
	{tz: "Africa/Johannesburg", date: "2000-Oct-13",
		lunar: "16, 17", tithi: "15, 16"},
	{tz: "America/Argentina/Buenos_Aires", date: "2000-Oct-13",
		lunar: "16, 17", tithi: "15, 16"},
	{tz: "Asia/Kolkata", date: "2000-Oct-21",
		lunar: "24", tithi: "23, 24, 25"},
	{tz: "America/Los_Angeles", date: "2000-Oct-21",
		lunar: "23, 24", tithi: "24, 25"},
	{tz: "Asia/Jakarta", date: "2000-Oct-21",
		lunar: "23, 24", tithi: "23, 24"},
	{tz: "America/Lima", date: "2000-Oct-21",
		lunar: "23, 24", tithi: "24, 25"},
	{tz: "Europe/Moscow", date: "2000-Oct-27",
		lunar: "29, 30, 1", tithi: "30, 1"},
	{tz: "America/Mexico_City", date: "2000-Oct-27",
		lunar: "29, 1, 2", tithi: "30, 1"},
	{tz: "Australia/Perth", date: "2000-Oct-27",
		lunar: "29, 30, 1", tithi: "30, 1"},
	{tz: "America/Santiago", date: "2000-Oct-27",
		lunar: "29, 1, 2", tithi: "30, 1"},
}

func TestCalendar(t *testing.T) {
	for _, tc := range calendarTests {
		loc, err := time.LoadLocation(tc.tz)
		if err != nil {
			t.Fatal(err)
		}
		lat, lon, ok := tzgeo.Lookup(tc.tz)
		if !ok {
			t.Fatalf("no coordinates for %s", tc.tz)
		}
		day, err := time.ParseInLocation(dateLayout, tc.date, loc)
		if err != nil {
			t.Fatal(err)
		}

		numbers := func(n int, number func(int) int) string {
			s := make([]string, n)
			for i := range s {
				s[i] = fmt.Sprint(number(i))
			}
			return strings.Join(s, ", ")
		}
		lunarDays, tithis := Calendar(day, lat, lon)
		if got := numbers(len(lunarDays), func(i int) int { return lunarDays[i].Index }); got != tc.lunar {
			t.Errorf("%s %s: lunar days = %q, want %q", tc.tz, tc.date, got, tc.lunar)
		}
		if got := numbers(len(tithis), func(i int) int { return tithis[i].Index }); got != tc.tithi {
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
