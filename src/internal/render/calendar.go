package render

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"
	"time"

	"kobo-dashboard/src/internal/model"
)

func period(w model.Widget, v model.View, now time.Time) (time.Time, int) {
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if w.Mode == "week" {
		d = d.AddDate(0, 0, -(int(d.Weekday())+6)%7+v.Offsets[w.ID]*7)
		return d, 1
	}
	d = time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, d.Location()).AddDate(0, v.Offsets[w.ID], 0)
	last := d.AddDate(0, 1, -1).Day()
	shift := (int(d.Weekday()) + 6) % 7
	rows := (shift + last + 6) / 7
	return d.AddDate(0, 0, -shift), rows
}
func selectedCalendar(w model.Widget, id string) bool {
	for _, v := range w.CalendarIDs {
		if v == id {
			return true
		}
	}
	return false
}
func eventTime(value string, allDay bool, loc *time.Location) (time.Time, error) {
	if allDay || len(value) == 10 {
		return time.ParseInLocation("2006-01-02", value, loc)
	}
	return time.Parse(time.RFC3339, value)
}

// eventsForDay handles end-exclusive all-day and timed events across midnight.
func eventsForDay(events []model.Event, w model.Widget, day time.Time) []model.Event {
	var result []model.Event
	next := day.AddDate(0, 0, 1)
	for _, e := range events {
		if !selectedCalendar(w, e.CalendarID) {
			continue
		}
		start, err := eventTime(e.Start, e.AllDay, day.Location())
		if err != nil {
			continue
		}
		end, err := eventTime(e.End, e.AllDay, day.Location())
		if err != nil || !end.After(start) {
			end = start.Add(time.Second)
		}
		if start.Before(next) && end.After(day) {
			result = append(result, e)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].AllDay != result[j].AllDay {
			return result[i].AllDay
		}
		return result[i].Start < result[j].Start
	})
	return result
}
func (c *canvas) calendar(w model.Widget, s model.Snapshot, v model.View, now time.Time, r image.Rectangle) {
	x, y, ww := r.Min.X, r.Min.Y, r.Dx()
	start, rows := period(w, v, now)
	label := start.AddDate(0, 0, 7).Format("January 2006")
	if w.Mode == "week" {
		label = start.Format("2 Jan") + " – " + start.AddDate(0, 0, 6).Format("2 Jan 2006")
	}
	fontSize := 44
	if ww < 800 {
		fontSize = 30
	}
	c.text(c.fit(label, ww-275, fontSize, true), x, y+49, fontSize, true, ink)
	for i, b := range []struct{ label, action string }{{"‹", "previous"}, {"Today", "today"}, {"›", "next"}} {
		bx := x + ww - 252 + i*83
		c.text(b.label, bx+12, y+43, 25, true, ink)
		c.hits = append(c.hits, model.Hit{X: bx, Y: y, W: 78, H: 65, Action: b.action, WidgetID: w.ID})
	}
	colW := ww / 7
	gy := y + 102
	bottom := r.Max.Y - 48
	cellH := (bottom - gy) / rows
	dateSize, dateBaseline, dateBox, eventTop := 26, 40, 49, 74
	if cellH < 100 {
		dateSize, dateBaseline, dateBox, eventTop = 18, 26, 32, 49
	}
	for i, label := range []string{"MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN"} {
		c.text(label, x+i*colW+10, y+87, 17, true, gray)
	}
	month := now.AddDate(0, v.Offsets[w.ID], 0).Month()
	if w.Mode == "month" {
		month = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, v.Offsets[w.ID], 0).Month()
	}
	for n := 0; n < rows*7; n++ {
		d := start.AddDate(0, 0, n)
		cx, cy := x+(n%7)*colW, gy+(n/7)*cellH
		c.line(cx, cy, colW, rule)
		if n%7 > 0 {
			c.fill(image.Rect(cx, cy, cx+1, cy+cellH), color.Gray{Y: 225})
		}
		col := color.Color(ink)
		if w.Mode == "month" && d.Month() != month {
			col = color.Gray{Y: 160}
		}
		if d.Format("2006-01-02") == now.Format("2006-01-02") {
			c.fill(image.Rect(cx+8, cy+6, cx+48, cy+dateBox), ink)
			c.text(fmt.Sprint(d.Day()), cx+14, cy+dateBaseline, dateSize, true, color.White)
		} else {
			c.text(fmt.Sprint(d.Day()), cx+12, cy+dateBaseline, dateSize, false, col)
		}
		events := eventsForDay(s.Events, w, d)
		textSize := 22
		if colW < 130 {
			textSize = 18
		}
		if cellH < 100 {
			textSize = 16
		}
		lineH := textSize + 7
		available := 1 + (cellH-eventTop-8)/lineH
		cursor := cy + eventTop
		for i, e := range events {
			if available < 1 {
				break
			}
			if i < len(events)-1 && available == 1 {
				c.text(fmt.Sprintf("+%d more", len(events)-i), cx+12, cursor, textSize, false, gray)
				break
			}
			// Use up to two lines per title; reserve one line for overflow.
			maxLines := 2
			if available == 1 || i < len(events)-1 && available == 2 {
				maxLines = 1
			}
			for _, line := range c.wrap(e.Title, colW-24, textSize, maxLines) {
				c.text(line, cx+12, cursor, textSize, false, col)
				cursor += lineH
				available--
			}
			// Keep one line available for a +N marker on crowded days.
		}
		c.hits = append(c.hits, model.Hit{X: cx, Y: cy, W: colW, H: cellH, Action: "day", WidgetID: w.ID, Value: d.Format("2006-01-02")})
	}
	status := model.Status{}
	for _, id := range w.CalendarIDs {
		st := s.Status[model.CalendarStatusID(id)]
		if status.UpdatedAt.IsZero() || st.UpdatedAt.Before(status.UpdatedAt) {
			status.UpdatedAt = st.UpdatedAt
		}
		if st.Error != "" {
			status.Error = st.Error
		}
	}
	if len(s.Calendars) == 0 && !s.Demo {
		c.text(c.fit("Connect Google Calendar with the setup helper", ww-240, 20, false), x, r.Max.Y-16, 20, false, gray)
	} else {
		c.status(status, x, r.Max.Y-16, ww-240)
	}
	for i, mode := range []string{"week", "month"} {
		mx := r.Max.X - 200 + i*105
		label := strings.ToUpper(mode)
		bold := mode == w.Mode
		c.text(label, mx, r.Max.Y-16, 17, bold, ink)
		c.hits = append(c.hits, model.Hit{X: mx - 10, Y: r.Max.Y - 45, W: 100, H: 45, Action: "mode", WidgetID: w.ID, Value: mode})
	}
}
func (c *canvas) wrap(s string, width, size, maxLines int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{"Untitled event"}
	}
	var lines []string
	line := ""
	for i, word := range words {
		candidate := strings.TrimSpace(line + " " + word)
		if c.measure(candidate, size, false) > width && line != "" {
			lines = append(lines, c.fit(line, width, size, false))
			line = word
		} else {
			line = candidate
		}
		if len(lines) == maxLines-1 {
			return append(lines, c.fit(strings.Join(append([]string{line}, words[i+1:]...), " "), width, size, false))
		}
	}
	if line != "" {
		lines = append(lines, c.fit(line, width, size, false))
	}
	return lines
}
func (c *canvas) dayOverlay(s model.Snapshot, v model.View, config model.Config) {
	var w model.Widget
	for _, a := range config.Widgets {
		if a.ID == v.WidgetID {
			w = a
			break
		}
	}
	if w.ID == "" {
		return
	}
	loc, _ := time.LoadLocation(config.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	day, err := time.ParseInLocation("2006-01-02", v.Day, loc)
	if err != nil {
		return
	}
	c.fill(image.Rect(74, 230, 1330, 1650), ink)
	c.fill(image.Rect(77, 233, 1327, 1647), color.White)
	c.text(day.Format("Monday, 2 January"), 120, 315, 43, true, ink)
	c.text("Close ×", 1120, 315, 27, true, ink)
	c.hits = []model.Hit{{X: 1080, Y: 250, W: 220, H: 100, Action: "close"}}
	c.line(120, 355, 1160, rule)
	events := eventsForDay(s.Events, w, day)
	y := 425
	if len(events) == 0 {
		c.text("A little room in your day.", 120, y, 30, false, gray)
	}
	pages := (len(events) + 5) / 6
	if pages < 1 {
		pages = 1
	}
	page := v.DayPage
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	start := page * 6
	end := start + 6
	if end > len(events) {
		end = len(events)
	}
	for _, e := range events[start:end] {
		lines := c.wrap(e.Title, 1100, 32, 3)
		for _, l := range lines {
			c.text(l, 120, y, 32, false, ink)
			y += 43
		}
		c.line(120, y+13, 1160, rule)
		y += 56
	}
	c.text(fmt.Sprintf("Page %d of %d", page+1, pages), 120, 1610, 23, false, gray)
	if page > 0 {
		c.text("‹ Previous", 850, 1610, 25, true, ink)
		c.hits = append(c.hits, model.Hit{X: 820, Y: 1555, W: 210, H: 80, Action: "day-previous", WidgetID: w.ID})
	}
	if page+1 < pages {
		c.text("Next ›", 1140, 1610, 25, true, ink)
		c.hits = append(c.hits, model.Hit{X: 1090, Y: 1555, W: 210, H: 80, Action: "day-next", WidgetID: w.ID})
	}
}
