# Tithi

**Tithi** is the current lunar day and *[tithi](https://en.wikipedia.org/wiki/Tithi)*
printer for a given date and location. Location is given either using the timezone format (e.g. `-tz="Europe/Berlin"`)
or using a pair of latitude and longitude coördinates (e.g. `-lat=-34` and `-lon=-58`, which is *Buenos Aires*).

```bash
$ tithi
```

<img width="1050" height="588" alt="image" src="https://github.com/user-attachments/assets/55e625fd-fca4-4869-b190-05f5fcfcdc8a" />


## Install

Requires Go version ≥ `1.25`.

```bash
$ go install github.com/pietv/tithi@latest
```

## Examples

With a different date:

```bash
$ tithi -date "Sep 12"
$ tithi -date "2021 Jan 1"
```

or location: `-lat` is latitude (positive number for the **N**orthern hemisphere, negative for the **S**outhern),
`-lon` is longitude (positive number fo the **E**astern hemisphere, negative for the **W**estern).

```bash
$ tithi -tz="America/Chicago"
$ tithi -tz="America/Argentina/Buenos_Aires"
$ tithi -lat=50 -lon=5
```

```bash
$ tithi -help

Usage: tithi [-tz <zone>] [-lat <deg>] [-lon <deg>] [-date DATE]

Print the lunar day for a specified date and location (print for today
and for the location set in the ‘TZ’ environment variable by default.)

  -date string
        date other than today (in format: “2026 Jan 01”, “Jan 01”, or “1 Jan”)
  -lat float
        latitude in degrees (+N, -S); default: current timezone (default NaN)
  -lon float
        longitude in degrees (+E, -W); default: current timezone (default NaN)
  -tz string
        timezone: IANA name, ±HH:MM, or empty for system (TZ="Europe/Berlin")
```
