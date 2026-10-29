package main

import (
	"embed"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/pietv/tithi"
	"github.com/xo/terminfo"
	"golang.org/x/term"
)

const (
	dateLayout  = "2006-Jan-2"
	shortLayout = "Jan _2 15:04 MST"
)

const tmpl = `{{define "header"}}Date: {{.Day}}  Location: -lat={{fmtlat .Lat}} -lon={{fmtlon .Lon}}{{with .IANA}} -tz={{printf "%q" .}}{{end}}{{end -}}
{{define "lunar"}}Lunar Days{{range .LunarDays}}

{{hl (current .Start .End) (printf "%d Lunar Day:\n%s\n%s" .Index (fmttime "start" .Start) (fmttime "end" .End))}}{{end}}{{end -}}
{{define "tithis"}}Tithis{{range .Tithis}}

{{hl (current .Start .End) (printf "%d %s (%s):\n%s\n%s" .Index (name .Index) (paksha .Index) (fmttime "start" .Start) (fmttime "end" .End))}}{{end}}{{end}}`

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

// newTemplate returns the report template, with times shown in loc and the
// interval running at now highlighted (when stdout is a color terminal).
func newTemplate(loc *time.Location, now time.Time) *template.Template {
	// Terminal color capabilities.
	var reverse, reset string
	if term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == "" {
		if info, err := terminfo.LoadFromEnv(); err == nil {
			reverse = info.Printf(terminfo.EnterBoldMode) + info.Printf(terminfo.EnterReverseMode)
			reset = info.Printf(terminfo.ExitAttributeMode)
		}
	}

	return template.Must(template.New("report").Funcs(template.FuncMap{
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
			day, _ := tithi.Name(k)
			return day
		},
		"paksha": func(k int) string {
			_, paksha := tithi.Name(k)
			return paksha
		},
		"current": func(start, end time.Time) bool {
			return !now.Before(start) && now.Before(end)
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
}

// moonDay returns the index of the lunar day running at now, or else of the
// first one shown, for choosing the moon picture.
func moonDay(lunarDays []tithi.Interval, now time.Time) int {
	day := 0
	for _, iv := range lunarDays {
		if day == 0 || (!now.Before(iv.Start) && now.Before(iv.End)) {
			day = iv.Index
		}
	}
	return day
}

// layout arranges the lunar days over the tithis, with the moon picture
// for lunar day n to their right.
func layout(lunar, tithis string, n int) string {
	box := lipgloss.NewStyle().Padding(0, 2)
	left := lipgloss.JoinVertical(lipgloss.Left,
		box.Render(lunar),
		"",
		box.Render(tithis))
	return lipgloss.JoinHorizontal(lipgloss.Center, left, box.Render(asciiMoon(n)))
}
