package bloomrules

import "time"

func Calendar(at time.Time, location *time.Location) Periods {
	if location == nil {
		location = time.UTC
	}
	year, month, date := at.In(location).Date()
	day := time.Date(year, month, date, 0, 0, 0, 0, time.UTC)
	daysSinceMonday := (int(day.Weekday()) + 6) % 7
	monday := day.AddDate(0, 0, -daysSinceMonday)
	dayStart, weekStart := localBoundary(day, location), localBoundary(monday, location)
	return Periods{
		Day:           Window{Start: dayStart, End: localBoundary(day.AddDate(0, 0, 1), location)},
		Week:          Window{Start: weekStart, End: localBoundary(monday.AddDate(0, 0, 7), location)},
		PriorWeek:     Window{Start: localBoundary(monday.AddDate(0, 0, -7), location), End: weekStart},
		CompletedDays: Window{Start: weekStart, End: dayStart},
	}
}

func localBoundary(day time.Time, location *time.Location) time.Time {
	year, month, date := day.Date()
	midnight := time.Date(year, month, date, 0, 0, 0, 0, location)
	if localDayNumber(midnight) < day.Unix()/86400 {
		_, before := midnight.Zone()
		_, after := midnight.Add(24 * time.Hour).Zone()
		return midnight.Add(time.Duration(after-before) * time.Second)
	}
	_, offset := midnight.Zone()
	_, earlierOffset := midnight.Add(-24 * time.Hour).Zone()
	earlier := midnight.Add(time.Duration(offset-earlierOffset) * time.Second)
	if earlier.Before(midnight) && localDayNumber(earlier) == localDayNumber(midnight) && earlier.Hour() == 0 && earlier.Minute() == 0 && earlier.Second() == 0 {
		return earlier
	}
	return midnight
}

// Aggregate counts requests in window and unique productive dates in its site's time zone.
func Aggregate(window Window, usage []Usage) Stats {
	var stats Stats
	dates := make(map[time.Time]struct{})
	location := window.Start.Location()
	for _, event := range usage {
		if event.At.Before(window.Start) || !event.At.Before(window.End) {
			continue
		}
		stats.Requests++
		if event.OutputTokens <= 0 && event.Images <= 0 {
			continue
		}
		stats.ProductiveRequests++
		stats.OutputTokens = add(stats.OutputTokens, max(0, event.OutputTokens))
		stats.Images = add(stats.Images, max(0, event.Images))
		year, month, day := event.At.In(location).Date()
		dates[time.Date(year, month, day, 0, 0, 0, 0, time.UTC)] = struct{}{}
	}
	stats.ActiveDays = int64(len(dates))
	return stats
}

func localDayNumber(at time.Time) int64 {
	year, month, day := at.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Unix() / 86400
}
