package automation

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Schedule kinds.
const (
	ScheduleManual   = "manual"   // only "Jetzt ausführen"
	ScheduleOnce     = "once"     // Date + At
	ScheduleDaily    = "daily"    // At
	ScheduleWeekly   = "weekly"   // Days + At
	ScheduleInterval = "interval" // every EveryMin minutes
)

const (
	minIntervalMin = 5
	maxIntervalMin = 7 * 24 * 60
)

// Schedule says when a task runs (local time).
type Schedule struct {
	Kind     string `json:"kind"`
	Date     string `json:"date,omitempty"`     // once: "2026-11-02"
	At       string `json:"at,omitempty"`       // "02:30"
	Days     []int  `json:"days,omitempty"`     // weekly: 1 = Montag … 7 = Sonntag
	EveryMin int    `json:"everyMin,omitempty"` // interval
}

var weekdayNames = []string{"", "Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"}

var clockRe = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)

func parseClock(at string) (int, int, error) {
	m := clockRe.FindStringSubmatch(at)
	if m == nil {
		return 0, 0, fmt.Errorf("Uhrzeit %q ungültig (HH:MM)", at)
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	return h, mi, nil
}

// Validate checks the schedule and normalises the weekday list.
func (s *Schedule) Validate() error {
	switch s.Kind {
	case ScheduleManual:
		return nil
	case ScheduleOnce:
		if _, err := time.ParseInLocation("2006-01-02", s.Date, time.Local); err != nil {
			return fmt.Errorf("Datum %q ungültig (JJJJ-MM-TT)", s.Date)
		}
		_, _, err := parseClock(s.At)
		return err
	case ScheduleDaily:
		_, _, err := parseClock(s.At)
		return err
	case ScheduleWeekly:
		seen := map[int]bool{}
		days := s.Days[:0]
		for _, d := range s.Days {
			if d >= 1 && d <= 7 && !seen[d] {
				seen[d] = true
				days = append(days, d)
			}
		}
		if len(days) == 0 {
			return errors.New("mindestens einen Wochentag wählen")
		}
		sort.Ints(days)
		s.Days = days
		_, _, err := parseClock(s.At)
		return err
	case ScheduleInterval:
		if s.EveryMin < minIntervalMin || s.EveryMin > maxIntervalMin {
			return fmt.Errorf("Intervall zwischen %d Minuten und 7 Tagen wählen", minIntervalMin)
		}
		return nil
	}
	return fmt.Errorf("unbekannter Zeitplan %q", s.Kind)
}

// Next returns the first run time strictly after t; false if there is none.
func (s Schedule) Next(t time.Time) (time.Time, bool) {
	switch s.Kind {
	case ScheduleInterval:
		if s.EveryMin <= 0 {
			return time.Time{}, false
		}
		return t.Add(time.Duration(s.EveryMin) * time.Minute).Truncate(time.Minute), true
	case ScheduleOnce:
		h, m, err := parseClock(s.At)
		d, derr := time.ParseInLocation("2006-01-02", s.Date, t.Location())
		if err != nil || derr != nil {
			return time.Time{}, false
		}
		at := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, t.Location())
		return at, at.After(t)
	case ScheduleDaily, ScheduleWeekly:
		h, m, err := parseClock(s.At)
		if err != nil {
			return time.Time{}, false
		}
		for i := 0; i <= 7; i++ {
			day := t.AddDate(0, 0, i)
			at := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, t.Location())
			if !at.After(t) {
				continue
			}
			if s.Kind == ScheduleDaily || s.hasDay(isoWeekday(at)) {
				return at, true
			}
		}
	}
	return time.Time{}, false
}

func isoWeekday(t time.Time) int {
	if wd := int(t.Weekday()); wd != 0 {
		return wd
	}
	return 7
}

func (s Schedule) hasDay(d int) bool {
	for _, x := range s.Days {
		if x == d {
			return true
		}
	}
	return false
}

// Describe returns a short German description ("Mo, Mi, Fr um 02:30").
func (s Schedule) Describe() string {
	switch s.Kind {
	case ScheduleManual:
		return "nur von Hand"
	case ScheduleOnce:
		if d, err := time.Parse("2006-01-02", s.Date); err == nil {
			return fmt.Sprintf("einmal am %s um %s", d.Format("02.01.2006"), s.At)
		}
	case ScheduleDaily:
		return "täglich um " + s.At
	case ScheduleWeekly:
		names := make([]string, 0, len(s.Days))
		for _, d := range s.Days {
			if d >= 1 && d <= 7 {
				names = append(names, weekdayNames[d])
			}
		}
		return strings.Join(names, ", ") + " um " + s.At
	case ScheduleInterval:
		if s.EveryMin%60 == 0 {
			return fmt.Sprintf("alle %d h", s.EveryMin/60)
		}
		return fmt.Sprintf("alle %d min", s.EveryMin)
	}
	return s.Kind
}
