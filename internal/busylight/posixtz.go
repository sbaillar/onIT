package busylight

import (
	"fmt"
	"strconv"
	"time"
)

// posixTZ derives the POSIX TZ string the ESP32 applies (setenv("TZ")) from
// loc's transitions during year: "EST5EDT,M3.2.0,M11.1.0" for the common
// two-transition std/dst case. Zones without DST that year — and anything
// unusual, like more than two transitions — fall back to a fixed-offset
// string for the zone in effect on Jan 1 (e.g. "MST7").
func posixTZ(loc *time.Location, year int) string {
	trans := zoneTransitions(loc, year)
	jan1Name, jan1Off := time.Date(year, 1, 1, 0, 0, 0, 0, loc).Zone()
	if len(trans) != 2 || trans[0].toOff == trans[1].toOff {
		return posixName(jan1Name) + posixOffset(jan1Off)
	}
	// std is the lower offset, dst the higher (the rule everywhere common).
	toDST, toSTD := trans[0], trans[1]
	if toDST.toOff < toSTD.toOff {
		toDST, toSTD = toSTD, toDST
	}
	s := posixName(toSTD.toName) + posixOffset(toSTD.toOff) + posixName(toDST.toName)
	if toDST.toOff != toSTD.toOff+3600 { // dst defaults to std+1h; else explicit
		s += posixOffset(toDST.toOff)
	}
	return s + "," + posixRule(toDST, year) + "," + posixRule(toSTD, year)
}

// zoneTransition is one UTC-offset change; when is the first instant of the
// new zone.
type zoneTransition struct {
	when    time.Time
	fromOff int
	toName  string
	toOff   int
}

// zoneTransitions finds loc's offset changes during year: a day-granularity
// scan, then a binary search to the exact second of each change.
func zoneTransitions(loc *time.Location, year int) []zoneTransition {
	var out []zoneTransition
	t := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
	_, prev := t.In(loc).Zone()
	for t.Before(end) {
		next := t.Add(24 * time.Hour)
		_, off := next.In(loc).Zone()
		if off != prev {
			lo, hi := t, next // the change is in (lo, hi]
			for hi.Sub(lo) > time.Second {
				mid := lo.Add(hi.Sub(lo) / 2).Truncate(time.Second)
				if _, o := mid.In(loc).Zone(); o == prev {
					lo = mid
				} else {
					hi = mid
				}
			}
			name, o := hi.In(loc).Zone()
			out = append(out, zoneTransition{when: hi, fromOff: prev, toName: name, toOff: o})
		}
		prev = off
		t = next
	}
	return out
}

// posixRule renders a transition as Mmonth.week.day[/time] in the wall-clock
// time in effect just before the transition, per POSIX. Week 5 means "last
// <day> of the month"; the /time is omitted at the 02:00:00 default.
func posixRule(tr zoneTransition, year int) string {
	lt := tr.when.UTC().Add(time.Duration(tr.fromOff) * time.Second)
	week := (lt.Day()-1)/7 + 1
	if lt.Day()+7 > daysIn(lt.Month(), year) {
		week = 5
	}
	s := fmt.Sprintf("M%d.%d.%d", lt.Month(), week, lt.Weekday())
	if h, m, sec := lt.Clock(); h != 2 || m != 0 || sec != 0 {
		s += "/" + strconv.Itoa(h)
		if m != 0 || sec != 0 {
			s += fmt.Sprintf(":%02d", m)
			if sec != 0 {
				s += fmt.Sprintf(":%02d", sec)
			}
		}
	}
	return s
}

func daysIn(m time.Month, year int) int {
	return time.Date(year, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// posixOffset renders a UTC offset (seconds) in POSIX TZ notation, which
// flips the sign: UTC-5 is "5", UTC+5:30 is "-5:30".
func posixOffset(off int) string {
	o := -off
	var s string
	if o < 0 {
		s, o = "-", -o
	}
	s += strconv.Itoa(o / 3600)
	if o%3600 != 0 {
		s += fmt.Sprintf(":%02d", o%3600/60)
		if o%60 != 0 {
			s += fmt.Sprintf(":%02d", o%60)
		}
	}
	return s
}

// posixName quotes zone abbreviations that aren't purely alphabetic, as
// POSIX requires (e.g. "-03" becomes "<-03>").
func posixName(name string) string {
	for _, r := range name {
		if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z') {
			return "<" + name + ">"
		}
	}
	return name
}
