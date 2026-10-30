# Tithi

[![Go Reference](https://pkg.go.dev/badge/github.com/pietv/tithi.svg)](https://pkg.go.dev/github.com/pietv/tithi)

**Tithi** is the current lunar day and *[tithi](https://en.wikipedia.org/wiki/Tithi)*
printer for a given date and location. Location is given either using the timezone format (e.g. `-tz="Europe/Berlin"`)
or using a pair of latitude and longitude coördinates (e.g. `-lat=-34` and `-lon=-58`, the coördinates of *Buenos Aires*).

* **Lunar days** are indexed from a new moon until the next moonrise (the 1st day), then from the previous
moonrise until the next, and, finally, from the last moonrise until the next new moon
(the last day: 29th or 30th). **Lunar days** depend on the geographical location because
they're tied to the horizon (the Moon rises at the horizon).

* **Tithis** are indexed from a new moon until the Moon's elongation from the Sun has increased by exactly 12°
(and there are 360° in a circle which makes 360/12 = 30 tithis per synodic month). A **tithi** can take
from 20 to 27 hours, and it doesn't depend on the geographical location because Sun-Earth-Moon angle
doesn't depend on the observer's horizon.

```bash
$ tithi
```

![](https://raw.githubusercontent.com/pietv/tithi/main/cmd/tithi/misc/demo.jpg)


## Install

Requires Go version ≥ `1.25`.

```bash
$ go install github.com/pietv/tithi/cmd/tithi@latest
```

## Examples

With a different date:

```bash
$ tithi -date "Sep 12"
$ tithi -date "2021 Jan 1"
```

With a different location as a timezone ([IANA](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones) format):

```bash
$ tithi -tz="America/Chicago"
$ tithi -tz="America/Argentina/Buenos_Aires"
```

With a different location as geocoördinates:

* `-lat` is latitude (positive number for the **N**orthern hemisphere, negative for the **S**outhern),
* `-lon` is longitude (positive number for the **E**astern hemisphere, negative for the **W**estern).

```bash
$ tithi -lat=-34.6 -lon=-58.4
```

## Usage

```bash
$ tithi -help
Usage: tithi [-tz <zone>] [-lat <deg>] [-lon <deg>] [-date DATE]

Print the lunar day for a specified date and location (print for today
and for the location set in the ‘TZ’ environment variable by default).
  -date string
        date other than today ("2026 Jan 01", "2026-Jan-01", "Jan 01", or "1 Jan")
  -lat float
        latitude in degrees (+N, -S); default: from the timezone
  -lon float
        longitude in degrees (+E, -W); default: from the timezone
  -tz string
        timezone: IANA name, or empty for system (TZ="Europe/Moscow")

```

## License

This project is licensed under the [MIT License](LICENSE).
