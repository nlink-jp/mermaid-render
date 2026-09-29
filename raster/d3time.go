package raster

import (
	"math"
	"strconv"
	"strings"
)

// The gantt axis follows d3 7.9.0: d3-scale 4.0.2's time scale ticks
// (d3-time 3.1.0's intervals, d3-array 3.2.4's tickStep) and d3-time-format
// 4.1.0's formatting in its default en-US locale. Dates are ms since the
// epoch in a browser set to UTC, as the gantt parser computes them.

const (
	durSecond = 1000
	durMinute = 60 * durSecond
	durHour   = 60 * durMinute
	durDay    = 24 * durHour
	durWeek   = 7 * durDay
	durMonth  = 30 * durDay
	durYear   = 365 * durDay
)

// d3Interval is a d3-time interval: floor to its boundary, and step.
type d3Interval struct {
	floor  func(ms float64) float64
	offset func(ms float64, n float64) float64
	field  func(ms float64) float64 // nil: every() counts from the epoch
	count  func(a, b float64) float64
}

func (iv *d3Interval) ceil(ms float64) float64 {
	d := iv.floor(ms - 1)
	d = iv.offset(d, 1)
	return iv.floor(d)
}

// every is interval.every(step).
func (iv *d3Interval) every(step float64) *d3Interval {
	step = math.Floor(step)
	if math.IsInf(step, 0) || math.IsNaN(step) || !(step > 0) {
		return nil
	}
	if !(step > 1) {
		return iv
	}
	test := func(d float64) bool {
		if iv.field != nil {
			return math.Mod(iv.field(d), step) == 0
		}
		return math.Mod(iv.count(0, d), step) == 0
	}
	return iv.filter(test)
}

func (iv *d3Interval) filter(test func(float64) bool) *d3Interval {
	return &d3Interval{
		floor: func(d float64) float64 {
			if d >= d {
				for d = iv.floor(d); !test(d); d = iv.floor(d - 1) {
				}
			}
			return d
		},
		offset: func(d, step float64) float64 {
			if d >= d {
				if step < 0 {
					for step++; step <= 0; step++ {
						for d = iv.offset(d, -1); !test(d); d = iv.offset(d, -1) {
						}
					}
				} else {
					for step--; step >= 0; step-- {
						for d = iv.offset(d, 1); !test(d); d = iv.offset(d, 1) {
						}
					}
				}
			}
			return d
		},
	}
}

// rangeOf is interval.range(start, stop): the boundaries in [start, stop).
func (iv *d3Interval) rangeOf(start, stop float64, limit int) []float64 {
	var out []float64
	start = iv.ceil(start)
	if !(start < stop) {
		return nil
	}
	for {
		prev := start
		out = append(out, prev)
		if len(out) > limit {
			return out
		}
		start = iv.floor(iv.offset(start, 1))
		if !(prev < start && start < stop) {
			return out
		}
	}
}

func fixedInterval(size float64) *d3Interval {
	return &d3Interval{
		floor:  func(d float64) float64 { return math.Floor(d/size) * size },
		offset: func(d, n float64) float64 { return d + n*size },
		count:  func(a, b float64) float64 { return (b - a) / size },
	}
}

var (
	d3Millisecond = &d3Interval{
		floor:  func(d float64) float64 { return d },
		offset: func(d, n float64) float64 { return d + n },
		count:  func(a, b float64) float64 { return b - a },
	}
	d3Second = withField(fixedInterval(durSecond), func(d float64) float64 { return float64(utcFields(d).s) })
	d3Minute = withField(fixedInterval(durMinute), func(d float64) float64 { return float64(utcFields(d).mi) })
	d3Hour   = withField(fixedInterval(durHour), func(d float64) float64 { return float64(utcFields(d).h) })
	d3Day    = withField(fixedInterval(durDay), func(d float64) float64 { return float64(utcFields(d).d - 1) })
	d3Month  = &d3Interval{
		floor: func(d float64) float64 {
			f := utcFields(d)
			return utcDate(f.y, f.mon, 1)
		},
		offset: func(d, n float64) float64 {
			f := utcFields(d)
			return utcDateTime(f.y, f.mon+int64(n), f.d, d-utcDate(f.y, f.mon, f.d))
		},
		field: func(d float64) float64 { return float64(utcFields(d).mon) },
		count: func(a, b float64) float64 {
			fa, fb := utcFields(a), utcFields(b)
			return float64(fb.mon - fa.mon + (fb.y-fa.y)*12)
		},
	}
	d3Year = &d3Interval{
		floor: func(d float64) float64 {
			return utcDate(utcFields(d).y, 0, 1)
		},
		offset: func(d, n float64) float64 {
			f := utcFields(d)
			return utcDateTime(f.y+int64(n), f.mon, f.d, d-utcDate(f.y, f.mon, f.d))
		},
		field: func(d float64) float64 { return float64(utcFields(d).y) },
		count: func(a, b float64) float64 { return float64(utcFields(b).y - utcFields(a).y) },
	}
)

func withField(iv *d3Interval, f func(float64) float64) *d3Interval {
	iv.field = f
	return iv
}

// weekdayInterval is timeSunday .. timeSaturday: weeks starting on day i
// (0 = Sunday), counted from the epoch.
func weekdayInterval(i int) *d3Interval {
	floor := func(d float64) float64 {
		day := math.Floor(d / durDay)
		wd := math.Mod(math.Mod(day+4, 7)+7, 7) // 1970-01-01 was a Thursday
		back := math.Mod(wd-float64(i)+7, 7)
		return (day - back) * durDay
	}
	return &d3Interval{
		floor:  floor,
		offset: func(d, n float64) float64 { return d + n*durWeek },
		count: func(a, b float64) float64 {
			return math.Floor((floor(b) - floor(a)) / durWeek)
		},
	}
}

// d3YearEvery is timeYear.every(k): years that are multiples of k.
func d3YearEvery(k float64) *d3Interval {
	k = math.Floor(k)
	if math.IsInf(k, 0) || math.IsNaN(k) || !(k > 0) {
		return nil
	}
	return &d3Interval{
		floor: func(d float64) float64 {
			y := utcFields(d).y
			return utcDate(int64(math.Floor(float64(y)/k)*k), 0, 1)
		},
		offset: func(d, n float64) float64 {
			f := utcFields(d)
			return utcDateTime(f.y+int64(n*k), f.mon, f.d, d-utcDate(f.y, f.mon, f.d))
		},
	}
}

func d3MillisecondEvery(k float64) *d3Interval {
	k = math.Floor(k)
	if math.IsInf(k, 0) || math.IsNaN(k) || !(k > 0) {
		return nil
	}
	if !(k > 1) {
		return d3Millisecond
	}
	return &d3Interval{
		floor:  func(d float64) float64 { return math.Floor(d/k) * k },
		offset: func(d, n float64) float64 { return d + n*k },
	}
}

type tickEntry struct {
	iv   *d3Interval
	step float64
	dur  float64
}

var tickIntervals = []tickEntry{
	{d3Second, 1, durSecond}, {d3Second, 5, 5 * durSecond}, {d3Second, 15, 15 * durSecond}, {d3Second, 30, 30 * durSecond},
	{d3Minute, 1, durMinute}, {d3Minute, 5, 5 * durMinute}, {d3Minute, 15, 15 * durMinute}, {d3Minute, 30, 30 * durMinute},
	{d3Hour, 1, durHour}, {d3Hour, 3, 3 * durHour}, {d3Hour, 6, 6 * durHour}, {d3Hour, 12, 12 * durHour},
	{d3Day, 1, durDay}, {d3Day, 2, 2 * durDay},
	{weekdayInterval(0), 1, durWeek},
	{d3Month, 1, durMonth}, {d3Month, 3, 3 * durMonth},
	{d3Year, 1, durYear},
}

// d3TickInterval is d3-time's tickInterval(start, stop, count).
func d3TickInterval(start, stop, count float64) *d3Interval {
	target := math.Abs(stop-start) / count
	i := 0
	for i < len(tickIntervals) && tickIntervals[i].dur <= target { // bisector.right
		i++
	}
	if i == len(tickIntervals) {
		return d3YearEvery(d3TickStep(start/durYear, stop/durYear, count))
	}
	if i == 0 {
		return d3MillisecondEvery(math.Max(d3TickStep(start, stop, count), 1))
	}
	e := tickIntervals[i]
	if target/tickIntervals[i-1].dur < tickIntervals[i].dur/target {
		e = tickIntervals[i-1]
	}
	return e.iv.every(e.step)
}

// d3Ticks is scaleTime().domain([start, stop]).ticks(interval or 10).
func d3Ticks(start, stop float64, iv *d3Interval, limit int) []float64 {
	if stop < start {
		start, stop = stop, start
	}
	if iv == nil {
		iv = d3TickInterval(start, stop, 10)
	}
	if iv == nil {
		return nil
	}
	return iv.rangeOf(start, stop+1, limit)
}

// d3TickStep is d3-array's tickStep.
func d3TickStep(start, stop, count float64) float64 {
	reverse := stop < start
	var inc float64
	if reverse {
		inc = d3TickIncrement(stop, start, count)
	} else {
		inc = d3TickIncrement(start, stop, count)
	}
	sign := 1.0
	if reverse {
		sign = -1
	}
	if inc < 0 {
		return sign * (1 / -inc)
	}
	return sign * inc
}

func d3TickIncrement(start, stop, count float64) float64 {
	_, _, inc := d3TickSpec(start, stop, count)
	return inc
}

func d3TickSpec(start, stop, count float64) (float64, float64, float64) {
	e10, e5, e2 := math.Sqrt(50), math.Sqrt(10), math.Sqrt(2)
	step := (stop - start) / math.Max(0, count)
	power := math.Floor(math.Log10(step))
	err := step / math.Pow(10, power)
	factor := 1.0
	switch {
	case err >= e10:
		factor = 10
	case err >= e5:
		factor = 5
	case err >= e2:
		factor = 2
	}
	var i1, i2, inc float64
	if power < 0 {
		inc = math.Pow(10, -power) / factor
		i1, i2 = jsRoundF(start*inc), jsRoundF(stop*inc)
		if i1/inc < start {
			i1++
		}
		if i2/inc > stop {
			i2--
		}
		inc = -inc
	} else {
		inc = math.Pow(10, power) * factor
		i1, i2 = jsRoundF(start/inc), jsRoundF(stop/inc)
		if i1*inc < start {
			i1++
		}
		if i2*inc > stop {
			i2--
		}
	}
	if i2 < i1 && 0.5 <= count && count < 2 {
		return d3TickSpec(start, stop, count*2)
	}
	return i1, i2, inc
}

func jsRoundF(v float64) float64 { return math.Floor(v + 0.5) }

// utcFields is a date's fields in UTC.
type utcF struct {
	y            int64
	mon          int64 // 0-11
	d            int64
	h, mi, s, ms int
	wd           int // 0 = Sunday
	days         int64
}

func utcFields(t float64) utcF {
	ms := int64(math.Floor(t))
	days := ms / durDay
	if ms%durDay < 0 {
		days--
	}
	rem := ms - days*durDay
	y, m, d := civil(days)
	return utcF{y: y, mon: int64(m - 1), d: int64(d), h: int(rem / durHour), mi: int(rem / durMinute % 60),
		s: int(rem / durSecond % 60), ms: int(rem % 1000), wd: int(((days % 7) + 7 + 4) % 7), days: days}
}

// utcDate is Date.UTC(y, mon, d) with overflow.
func utcDate(y, mon, d int64) float64 {
	y += mon / 12
	mon %= 12
	if mon < 0 {
		mon += 12
		y--
	}
	return float64(daysCivil(y, int(mon)+1, 1)+d-1) * durDay
}

func utcDateTime(y, mon, d int64, timeOfDay float64) float64 {
	return utcDate(y, mon, d) + timeOfDay
}

func daysCivil(y int64, m, d int) int64 {
	if m <= 2 {
		y--
	}
	era := y / 400
	if y < 0 && y%400 != 0 {
		era--
	}
	yoe := y - era*400
	doy := int64((153*((m+9)%12)+2)/5 + d - 1)
	return era*146097 + yoe*365 + yoe/4 - yoe/100 + doy - 719468
}

func civil(z int64) (int64, int, int) {
	z += 719468
	era := z / 146097
	if z < 0 && z%146097 != 0 {
		era--
	}
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := int(doy - (153*mp+2)/5 + 1)
	m := int(mp + 3)
	if m > 12 {
		m -= 12
	}
	if m <= 2 {
		y++
	}
	return y, m, d
}

var (
	d3Days        = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	d3ShortDays   = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	d3Months      = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	d3ShortMonths = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

// d3Format is d3-time-format's timeFormat(specifier)(date), en-US.
func d3Format(spec string, t float64) string {
	var b strings.Builder
	rs := []rune(spec)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '%' {
			b.WriteRune(rs[i])
			continue
		}
		i++
		if i >= len(rs) {
			break
		}
		c := rs[i]
		pad, hasPad := rune(0), false
		if c == '-' || c == '_' || c == '0' {
			pad, hasPad = map[rune]rune{'-': 0, '_': ' ', '0': '0'}[c], true
			i++
			if i >= len(rs) {
				break
			}
			c = rs[i]
		}
		if !hasPad {
			pad = '0'
			if c == 'e' {
				pad = ' '
			}
		}
		b.WriteString(d3Directive(c, pad, t))
	}
	return b.String()
}

func d3Pad(v int64, pad rune, width int) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	s := strconv.FormatInt(v, 10)
	if pad != 0 && len(s) < width {
		s = strings.Repeat(string(pad), width-len(s)) + s
	}
	return sign + s
}

func d3Directive(c, pad rune, t float64) string {
	f := utcFields(t)
	switch c {
	case 'a':
		return d3ShortDays[f.wd]
	case 'A':
		return d3Days[f.wd]
	case 'b', 'h':
		return d3ShortMonths[f.mon]
	case 'B':
		return d3Months[f.mon]
	case 'c':
		return d3Format("%x, %X", t)
	case 'd':
		return d3Pad(f.d, pad, 2)
	case 'e':
		return d3Pad(f.d, pad, 2)
	case 'f':
		return d3Pad(int64(f.ms)*1000, pad, 6)
	case 'g':
		_, y := isoWeekOf(f)
		return d3Pad(y%100, pad, 2)
	case 'G':
		_, y := isoWeekOf(f)
		return d3Pad(y%10000, pad, 4)
	case 'H':
		return d3Pad(int64(f.h), pad, 2)
	case 'I':
		h := f.h % 12
		if h == 0 {
			h = 12
		}
		return d3Pad(int64(h), pad, 2)
	case 'j':
		jan1 := daysCivil(f.y, 1, 1)
		return d3Pad(f.days-jan1+1, pad, 3)
	case 'L':
		return d3Pad(int64(f.ms), pad, 3)
	case 'm':
		return d3Pad(f.mon+1, pad, 2)
	case 'M':
		return d3Pad(int64(f.mi), pad, 2)
	case 'p':
		if f.h >= 12 {
			return "PM"
		}
		return "AM"
	case 'q':
		return strconv.FormatInt(1+f.mon/3, 10)
	case 'Q':
		return strconv.FormatInt(int64(math.Floor(t)), 10)
	case 's':
		return strconv.FormatInt(int64(math.Floor(t/1000)), 10)
	case 'S':
		return d3Pad(int64(f.s), pad, 2)
	case 'u':
		wd := f.wd
		if wd == 0 {
			wd = 7
		}
		return strconv.Itoa(wd)
	case 'U':
		jan1 := daysCivil(f.y, 1, 1)
		sunday := weekdayInterval(0)
		return d3Pad(int64(sunday.count(float64(jan1)*durDay-1, t)), pad, 2)
	case 'V':
		w, _ := isoWeekOf(f)
		return d3Pad(int64(w), pad, 2)
	case 'w':
		return strconv.Itoa(f.wd)
	case 'W':
		jan1 := daysCivil(f.y, 1, 1)
		monday := weekdayInterval(1)
		return d3Pad(int64(monday.count(float64(jan1)*durDay-1, t)), pad, 2)
	case 'x':
		return d3Format("%-m/%-d/%Y", t)
	case 'X':
		return d3Format("%-I:%M:%S %p", t)
	case 'y':
		return d3Pad(f.y%100, pad, 2) // a year before 0 keeps its sign, as d3's pad does
	case 'Y':
		return d3Pad(f.y%10000, pad, 4)
	case 'Z':
		return "+0000"
	case '%':
		return "%"
	}
	return string(c)
}

// isoWeekOf is the ISO week number and year.
func isoWeekOf(f utcF) (int, int64) {
	iso := f.wd
	if iso == 0 {
		iso = 7
	}
	thursday := f.days - int64(iso) + 4
	y, _, _ := civil(thursday)
	return int((thursday-daysCivil(y, 1, 1))/7) + 1, y
}
