package mermaidrender

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// The gantt chart reads and writes dates with dayjs 1.11.21 (with the
// customParseFormat, advancedFormat and isoWeek plugins, English locale),
// the version mermaid 12.0.0 locks. This is a port of what ganttDb uses:
// the strict parse (parse by the format, then the date formatted back must
// equal the input), format, and add. Dates are JavaScript Dates in a
// browser set to UTC: milliseconds since the epoch, no daylight saving.

// jsDate is a JavaScript Date: ms since the epoch, or invalid.
type jsDate struct {
	ms    int64
	valid bool
}

const jsMaxTime = 8.64e15

var jsInvalid = jsDate{}

func jsTimeClip(ms float64) jsDate {
	if math.IsNaN(ms) || math.IsInf(ms, 0) || math.Abs(ms) > jsMaxTime {
		return jsInvalid
	}
	return jsDate{ms: int64(ms), valid: true} // ToIntegerOrInfinity: toward zero
}

// jsMakeDate is new Date(y, M, d, h, m, s, ms) (and Date.UTC, the same in
// UTC): a year 0-99 is 1900 + it; months and days overflow into the next.
func jsMakeDate(y, mon, d, h, mi, s, ms float64) jsDate {
	for _, v := range []float64{y, mon, d, h, mi, s, ms} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return jsInvalid
		}
	}
	y, mon, d = math.Trunc(y), math.Trunc(mon), math.Trunc(d)
	h, mi, s, ms = math.Trunc(h), math.Trunc(mi), math.Trunc(s), math.Trunc(ms)
	if y >= 0 && y <= 99 {
		y += 1900
	}
	ym := y + math.Floor(mon/12)
	mn := math.Mod(mon, 12)
	if mn < 0 {
		mn += 12
	}
	if math.Abs(ym) > 400000 {
		return jsInvalid
	}
	days := float64(daysFromCivil(int64(ym), int(mn)+1, 1)) + d - 1
	t := days*86400000 + h*3600000 + mi*60000 + s*1000 + ms
	return jsTimeClip(t)
}

// daysFromCivil is the day number (0 = 1970-01-01) of a proleptic
// Gregorian date (Howard Hinnant's algorithm).
func daysFromCivil(y int64, m, d int) int64 {
	if m <= 2 {
		y--
	}
	era := y / 400
	if y < 0 && y%400 != 0 {
		era--
	}
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := int64((153*mp+2)/5 + d - 1)
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func civilFromDays(z int64) (y int64, m, d int) {
	z += 719468
	era := z / 146097
	if z < 0 && z%146097 != 0 {
		era--
	}
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y = yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d = int(doy - (153*mp+2)/5 + 1)
	m = int(mp + 3)
	if m > 12 {
		m -= 12
	}
	if m <= 2 {
		y++
	}
	return y, m, d
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// jsFields are a Date's calendar fields in UTC.
type jsFields struct {
	y            int64
	mon, d       int // mon 0-11
	h, mi, s, ms int
	weekday      int // 0 = Sunday
	days         int64
}

func (t jsDate) fields() jsFields {
	days := floorDiv(t.ms, 86400000)
	rem := t.ms - days*86400000
	y, m, d := civilFromDays(days)
	wd := int((days%7 + 7 + 4) % 7)
	return jsFields{y: y, mon: m - 1, d: d, h: int(rem / 3600000), mi: int(rem / 60000 % 60),
		s: int(rem / 1000 % 60), ms: int(rem % 1000), weekday: wd, days: days}
}

func (t jsDate) addDays(n int64) jsDate {
	if !t.valid {
		return t
	}
	return jsTimeClip(float64(t.ms) + float64(n)*86400000)
}

// djFill says which fields a parse took from today (customParseFormat
// fills a missing year, and a missing month and day, from the current
// date).
type djFill struct{ year, month, day bool }

func (f djFill) any() bool { return f.year || f.month || f.day }

// djRef is the date the parser treats as today: nothing drawn may depend
// on it (the caller refuses what would).
var djRef = jsFields{y: 2000, mon: 0, d: 1}

var (
	djTokens     = regexp.MustCompile(`(\[[^[]*\])|([-_:/.,()` + jsSpace + `]+)|(A|a|Q|YYYY|YY?|ww?|MM?M?M?|Do|DD?|hh?|HH?|mm?|ss?|S{1,3}|z|ZZ?)`)
	djLocalized  = regexp.MustCompile(`(\[[^\]]+])|(LTS?|l{1,4}|L{1,4})`)
	djMatch1     = regexp.MustCompile(`\d`)
	djMatch2     = regexp.MustCompile(`\d\d`)
	djMatch3     = regexp.MustCompile(`\d{3}`)
	djMatch4     = regexp.MustCompile(`\d{4}`)
	djMatch1to2  = regexp.MustCompile(`\d\d?`)
	djSigned     = regexp.MustCompile(`[+-]?\d+`)
	djOffset     = regexp.MustCompile(`[+-]\d\d:?(\d\d)?|Z`)
	djWord       = regexp.MustCompile(`\d*[^-_:/,()` + jsSpace + `\d]+`)
	djDigits     = regexp.MustCompile(`\d+`)
	djOffsetPart = regexp.MustCompile(`([+-]|\d\d)`)
)

var djMonths = []string{"January", "February", "March", "April", "May", "June", "July",
	"August", "September", "October", "November", "December"}
var djWeekdays = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

func djOrdinal(n int) string {
	s := []string{"th", "st", "nd", "rd"}
	v := n % 100
	suf := ""
	if i := (v - 20) % 10; i >= 0 && i < 4 {
		suf = s[i]
	}
	if suf == "" && v >= 0 && v < 4 {
		suf = s[v]
	}
	if suf == "" {
		suf = s[0]
	}
	return "[" + strconv.Itoa(n) + suf + "]"
}

// errDjThrows is a format dayjs throws on (a token whose plugin mermaid
// does not load), so mermaid fails to draw the chart.
var errDjThrows = errors.New("the date format uses a token dayjs cannot handle here")

// djParseStrict is dayjs(input, format, true): the date, whether it is
// valid, and the fields taken from today.
func djParseStrict(input, format string) (jsDate, djFill, error) {
	d, fill := djParseFormatted(input, format)
	out, err := djFormat(d, format)
	if err != nil {
		return jsInvalid, fill, err
	}
	if out != input {
		return jsInvalid, fill, nil
	}
	return d, fill, nil
}

// djParseFormatted is customParseFormat's parseFormattedInput.
func djParseFormatted(input, format string) (jsDate, djFill) {
	if format == "" {
		return jsInvalid, djFill{} // ''.match() is null: makeParser throws
	}
	if format == "x" || format == "X" {
		n := jsNumberOf(input)
		if format == "X" {
			n *= 1000
		}
		return jsTimeClip(n), djFill{}
	}
	// localizedFormat's u() reads locale.formats, which the English locale
	// lacks: an L token throws, and the parse is invalid.
	for _, m := range djLocalized.FindAllStringSubmatch(format, -1) {
		if m[2] != "" {
			return jsInvalid, djFill{}
		}
	}
	toks := djTokens.FindAllString(format, -1)
	var (
		year, month, hours, minutes, seconds, ms, week float64
		day                                            float64
		hasZone                                        bool
		zoneMin                                        float64
		afternoon                                      = -1 // unset
	)
	in := utf16.Encode([]rune(input))
	start := 0
	for _, tok := range toks {
		re, set := djExpression(tok)
		if re == nil {
			lit := tok
			if strings.HasPrefix(lit, "[") {
				lit = lit[1:]
			}
			lit = strings.TrimSuffix(lit, "]")
			start += len(utf16.Encode([]rune(lit)))
			continue
		}
		part := ""
		if start < len(in) {
			part = string(utf16.Decode(in[start:]))
		}
		loc := re.FindStringIndex(part)
		if loc == nil {
			return jsInvalid, djFill{} // match[0] of null throws
		}
		value := part[loc[0]:loc[1]]
		switch set {
		case "A", "a":
			want := "PM"
			if set == "a" {
				want = "pm"
			}
			if value == want {
				afternoon = 1
			} else {
				afternoon = 0
			}
		case "Q":
			month = (num(value)-1)*3 + 1
		case "S":
			ms = num(value) * 100
		case "SS":
			ms = num(value) * 10
		case "SSS":
			ms = num(value)
		case "s":
			seconds = num(value)
		case "m":
			minutes = num(value)
		case "H":
			hours = num(value)
		case "D":
			day = num(value)
		case "Do":
			dg := djDigits.FindString(value)
			if dg == "" {
				return jsInvalid, djFill{}
			}
			day = num(dg)
			for i := 1; i <= 31; i++ {
				if strings.NewReplacer("[", "", "]", "").Replace(djOrdinal(i)) == value {
					day = float64(i)
				}
			}
		case "w":
			week = num(value)
		case "M":
			month = num(value)
		case "MMM", "MMMM":
			idx := -1
			for i, name := range djMonths {
				if set == "MMM" && name[:3] == value || set == "MMMM" && name == value {
					idx = i
					break
				}
			}
			if idx < 0 {
				return jsInvalid, djFill{}
			}
			month = float64(idx + 1)
		case "Y":
			year = num(value)
		case "YY":
			v := num(value)
			if v > 68 {
				year = v + 1900
			} else {
				year = v + 2000
			}
		case "YYYY":
			year = num(value)
		case "Z":
			hasZone = true
			zoneMin = djOffsetMinutes(value)
		}
		in = utf16.Encode([]rune(strings.Replace(string(utf16.Decode(in)), value, "", 1)))
	}
	if afternoon == 1 && hours < 12 {
		hours += 12
	} else if afternoon == 0 && hours == 12 {
		hours = 0
	}
	fill := djFill{}
	y := year
	if y == 0 {
		y, fill.year = float64(djRef.y), true
	}
	mon := 0.0
	if !(year != 0 && month == 0) {
		if month > 0 {
			mon = month - 1
		} else {
			mon, fill.month = float64(djRef.mon), true
		}
	}
	d := day
	if d == 0 {
		if year == 0 && month == 0 {
			d, fill.day = float64(djRef.d), true
		} else {
			d = 1
		}
	}
	if hasZone {
		return jsMakeDate(y, mon, d, hours, minutes, seconds, ms+zoneMin*60000), fill
	}
	if week != 0 {
		return jsInvalid, fill // dayjs(...).week() needs a plugin mermaid does not load
	}
	return jsMakeDate(y, mon, d, hours, minutes, seconds, ms), fill
}

func num(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// djExpression is the parse regex of a format token and the field it
// sets, or nil for a literal.
func djExpression(tok string) (*regexp.Regexp, string) {
	switch tok {
	case "A", "a":
		return djWord, tok
	case "Q":
		return djMatch1, "Q"
	case "S":
		return djMatch1, "S"
	case "SS":
		return djMatch2, "SS"
	case "SSS":
		return djMatch3, "SSS"
	case "s", "ss":
		return djMatch1to2, "s"
	case "m", "mm":
		return djMatch1to2, "m"
	case "H", "h", "HH", "hh":
		return djMatch1to2, "H"
	case "D":
		return djMatch1to2, "D"
	case "DD":
		return djMatch2, "D"
	case "Do":
		return djWord, "Do"
	case "w":
		return djMatch1to2, "w"
	case "ww":
		return djMatch2, "w"
	case "M":
		return djMatch1to2, "M"
	case "MM":
		return djMatch2, "M"
	case "MMM":
		return djWord, "MMM"
	case "MMMM":
		return djWord, "MMMM"
	case "Y":
		return djSigned, "Y"
	case "YY":
		return djMatch2, "YY"
	case "YYYY":
		return djMatch4, "YYYY"
	case "Z", "ZZ":
		return djOffset, "Z"
	}
	return nil, ""
}

func djOffsetMinutes(s string) float64 {
	if s == "" || s == "Z" {
		return 0
	}
	parts := djOffsetPart.FindAllString(s, -1)
	if len(parts) < 2 {
		return 0
	}
	minutes := num(parts[1]) * 60
	if len(parts) > 2 {
		minutes += num(parts[2])
	}
	if minutes == 0 {
		return 0
	}
	if parts[0] == "+" {
		return -minutes
	}
	return minutes
}

// jsNumberOf is JavaScript's Number(string).
func jsNumberOf(s string) float64 {
	s = strings.Trim(s, jsSpaceChars)
	if s == "" {
		return 0
	}
	low := strings.ToLower(s)
	for _, p := range []struct {
		prefix string
		base   int
	}{{"0x", 16}, {"0o", 8}, {"0b", 2}} {
		if strings.HasPrefix(low, p.prefix) {
			v, err := strconv.ParseUint(s[2:], p.base, 64)
			if err != nil {
				return math.NaN()
			}
			return float64(v)
		}
	}
	switch s {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if strings.ContainsAny(low, "infxn_") {
		return math.NaN()
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) && ne.Err == strconv.ErrRange {
			return v
		}
		return math.NaN()
	}
	return v
}

var (
	djAdvanced = regexp.MustCompile(`\[([^\]]+)]|Q|wo|ww|w|WW|W|zzz|z|gggg|GGGG|Do|X|x|k{1,2}|S`)
	djCore     = regexp.MustCompile(`\[([^\]]+)]|YYYY|YY|M{1,4}|D{1,2}|d{1,4}|H{1,2}|h{1,2}|a|A|m{1,2}|s{1,2}|Z{1,2}|SSS`)
)

// djPad is dayjs's Utils.s: the string padded at the front to length.
func djPad(v string, length int) string {
	if v == "" || len(v) >= length {
		return v
	}
	return strings.Repeat("0", length-len(v)) + v
}

// djFormat is dayjs's format with advancedFormat (English, UTC).
func djFormat(t jsDate, format string) (string, error) {
	if !t.valid {
		return "Invalid Date", nil
	}
	if format == "" {
		format = "YYYY-MM-DDTHH:mm:ssZ"
	}
	f := t.fields()
	var ferr error
	str := djAdvanced.ReplaceAllStringFunc(format, func(m string) string {
		switch m {
		case "Q":
			return strconv.Itoa((f.mon + 1 + 2) / 3)
		case "Do":
			return djOrdinal(f.d)
		case "GGGG":
			_, y := isoWeek(f)
			return strconv.FormatInt(y, 10)
		case "W", "WW":
			w, _ := isoWeek(f)
			if m == "W" {
				return strconv.Itoa(w)
			}
			return djPad(strconv.Itoa(w), 2)
		case "k", "kk":
			h := f.h
			if h == 0 {
				h = 24
			}
			if m == "k" {
				return strconv.Itoa(h)
			}
			return djPad(strconv.Itoa(h), 2)
		case "X":
			return strconv.FormatInt(floorDiv(t.ms, 1000), 10)
		case "x":
			return strconv.FormatInt(t.ms, 10)
		case "wo", "ww", "w", "gggg", "z", "zzz":
			ferr = errDjThrows
			return m
		}
		return m // brackets and S stay for the core pass
	})
	if ferr != nil {
		return "", ferr
	}
	h12 := f.h % 12
	if h12 == 0 {
		h12 = 12
	}
	out := djCore.ReplaceAllStringFunc(str, func(m string) string {
		if strings.HasPrefix(m, "[") {
			return m[1 : len(m)-1]
		}
		switch m {
		case "YY":
			ys := strconv.FormatInt(f.y, 10)
			if len(ys) > 2 {
				ys = ys[len(ys)-2:]
			}
			return ys
		case "YYYY":
			return djPad(strconv.FormatInt(f.y, 10), 4)
		case "M":
			return strconv.Itoa(f.mon + 1)
		case "MM":
			return djPad(strconv.Itoa(f.mon+1), 2)
		case "MMM":
			return djMonths[f.mon][:3]
		case "MMMM":
			return djMonths[f.mon]
		case "D":
			return strconv.Itoa(f.d)
		case "DD":
			return djPad(strconv.Itoa(f.d), 2)
		case "d":
			return strconv.Itoa(f.weekday)
		case "dd":
			return djWeekdays[f.weekday][:2]
		case "ddd":
			return djWeekdays[f.weekday][:3]
		case "dddd":
			return djWeekdays[f.weekday]
		case "H":
			return strconv.Itoa(f.h)
		case "HH":
			return djPad(strconv.Itoa(f.h), 2)
		case "h":
			return strconv.Itoa(h12)
		case "hh":
			return djPad(strconv.Itoa(h12), 2)
		case "a", "A":
			s := "AM"
			if f.h >= 12 {
				s = "PM"
			}
			if m == "a" {
				s = strings.ToLower(s)
			}
			return s
		case "m":
			return strconv.Itoa(f.mi)
		case "mm":
			return djPad(strconv.Itoa(f.mi), 2)
		case "s":
			return strconv.Itoa(f.s)
		case "ss":
			return djPad(strconv.Itoa(f.s), 2)
		case "SSS":
			return djPad(strconv.Itoa(f.ms), 3)
		case "Z":
			return "+00:00"
		}
		// ZZ, and MMMM's longer runs are split by the regex already.
		return "+0000"
	})
	return out, nil
}

// isoWeek is the ISO 8601 week number and its year.
func isoWeek(f jsFields) (int, int64) {
	iso := f.weekday
	if iso == 0 {
		iso = 7
	}
	thursday := f.days - int64(iso) + 4
	y, _, _ := civilFromDays(thursday)
	jan1 := daysFromCivil(y, 1, 1)
	return int((thursday-jan1)/7) + 1, y
}

// djAdd is dayjs's add(value, unit) for the units ganttDb's durations use.
func djAdd(t jsDate, v float64, unit string) jsDate {
	if !t.valid {
		return t
	}
	switch unit {
	case "M", "y":
		// set(M|Y): from the 1st, setMonth/setFullYear (the argument
		// truncated), then the day clamped to the new month's length.
		f := t.fields()
		y, mon := float64(f.y), float64(f.mon)
		if unit == "M" {
			mon = math.Trunc(mon + v)
		} else {
			y = math.Trunc(y + v)
		}
		first := f
		first.d = 1
		moved := jsSetYearMonth(first, y, mon)
		if !moved.valid {
			return moved
		}
		mf := moved.fields()
		return jsSetDay(mf, min(f.d, daysInMonth(mf.y, mf.mon)))
	case "d":
		return t.addDays(int64(jsRound(v)))
	case "w":
		return t.addDays(int64(jsRound(7 * v)))
	}
	step := map[string]float64{"h": 3600000, "m": 60000, "s": 1000, "ms": 1}[unit]
	return jsTimeClip(float64(t.ms) + v*step)
}

// jsSetYearMonth is setFullYear/setMonth on a date's fields.
func jsSetYearMonth(f jsFields, y, mon float64) jsDate {
	ym := y + math.Floor(mon/12)
	mn := math.Mod(mon, 12)
	if mn < 0 {
		mn += 12
	}
	if math.Abs(ym) > 400000 {
		return jsInvalid
	}
	d := float64(daysFromCivil(int64(ym), int(mn)+1, 1)) + float64(f.d) - 1
	return jsTimeClip(d*86400000 + float64(f.h)*3600000 + float64(f.mi)*60000 + float64(f.s)*1000 + float64(f.ms))
}

func jsSetDay(f jsFields, d int) jsDate {
	days := daysFromCivil(f.y, f.mon+1, 1) + int64(d) - 1
	return jsTimeClip(float64(days)*86400000 + float64(f.h)*3600000 + float64(f.mi)*60000 + float64(f.s)*1000 + float64(f.ms))
}

func daysInMonth(y int64, mon int) int {
	return int(daysFromCivil(y+int64((mon+1)/12), (mon+1)%12+1, 1) - daysFromCivil(y, mon+1, 1))
}

// jsRound is Math.round: halves go up.
func jsRound(v float64) float64 { return math.Floor(v + 0.5) }
