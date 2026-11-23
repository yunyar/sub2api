package bloomrules

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"time"
)

const DefaultWeeklyBudgetMicros int64 = 10_000_000

func WeeklyBudgetMicros(priorWeekRequests, priorWeekWalletSpendMicros int64) int64 {
	if priorWeekRequests <= 0 {
		return DefaultWeeklyBudgetMicros
	}
	return max(0, priorWeekWalletSpendMicros) / 10
}

func RewardMicros(tier Tier, weeklyBudgetMicros int64) int64 {
	rates := map[Tier]int64{Easy: 1, Medium: 3, Hard: 6, Weekly: 30}
	return percent(max(0, weeklyBudgetMicros), rates[tier])
}

func percent(value, rate int64) int64 {
	return value/100*rate + value%100*rate/100
}

// Issue generates issuance candidates. Reuse the persisted Challenge for an
// already-issued account, tier and period to keep its target fixed.
func Issue(input IssueInput) Assignment {
	periods := Calendar(input.At, input.Location)
	return issueForDay(input, periods, periods.Day)
}

func IssueWeek(input IssueInput) [7]Assignment {
	periods := Calendar(input.At, input.Location)
	year, month, date := periods.Week.Start.Date()
	monday := time.Date(year, month, date, 0, 0, 0, 0, time.UTC)
	var schedule [7]Assignment
	for index := range schedule {
		location := periods.Week.Start.Location()
		day := Window{Start: localBoundary(monday.AddDate(0, 0, index), location),
			End: localBoundary(monday.AddDate(0, 0, index+1), location)}
		schedule[index] = issueForDay(input, periods, day)
	}
	return schedule
}

func issueForDay(input IssueInput, periods Periods, day Window) Assignment {
	budget := WeeklyBudgetMicros(input.PriorWeek.Requests, input.PriorWeekWalletSpendMicros)
	result := Assignment{WeeklyBudgetMicros: budget}
	completedDays := localDayNumber(periods.Day.Start) - localDayNumber(periods.Week.Start)
	dailyHistory, baselineDays := input.PriorWeek, int64(7)
	dailyModality := Classify(input.PriorWeek)
	if completedDays > 0 {
		dailyHistory, baselineDays = input.CompletedDays, completedDays
		if modality := Classify(dailyHistory); modality != ColdStart {
			dailyModality = modality
		}
	}
	for index, tier := range [...]Tier{Easy, Medium, Hard} {
		result.Daily[index] = issueOne(input.AccountID, tier, day, dailyHistory, dailyModality, baselineDays, budget)
	}
	result.Weekly = issueOne(input.AccountID, Weekly, periods.Week, input.PriorWeek, Classify(input.PriorWeek), 7, budget)
	return result
}

func issueOne(accountID int64, tier Tier, window Window, history Stats, modality Modality, baselineDays, budget int64) Challenge {
	seed := binary.BigEndian.AppendUint64([]byte(tier), uint64(accountID))
	digest := sha256.Sum256(seed)
	offset := int64(binary.BigEndian.Uint64(digest[:8]) % templatesPerTier)
	period := localDayNumber(window.Start)
	if tier == Weekly {
		period = (period - 4) / 7
	}
	index := ((period%templatesPerTier)*17 + offset + templatesPerTier*17) % templatesPerTier
	template := templateAt(tier, index)
	metric := template.MetricFor(modality)
	return Challenge{
		TemplateID:   template.ID,
		Tier:         tier,
		Metric:       metric,
		Target:       targetFor(tier, metric, template.Intensity, history, baselineDays),
		Window:       window,
		RewardMicros: RewardMicros(tier, budget),
	}
}

func Classify(stats Stats) Modality {
	switch {
	case stats.OutputTokens > 0 && stats.Images > 0:
		return Mixed
	case stats.OutputTokens > 0:
		return Text
	case stats.Images > 0:
		return Image
	default:
		return ColdStart
	}
}

// Progress accepts stats restricted to challenge.Window. checkedIn describes
// a persisted check-in for that same day.
func Progress(challenge Challenge, stats Stats, checkedIn bool) ProgressResult {
	var current int64
	switch {
	case challenge.Metric == CheckIn:
		if challenge.Tier != Easy {
			return ProgressResult{}
		}
		if checkedIn {
			current = 1
		}
	case stats.ProductiveRequests > 0:
		switch challenge.Metric {
		case ProductiveRequests:
			current = stats.ProductiveRequests
		case OutputTokens:
			current = stats.OutputTokens
		case Images:
			current = stats.Images
		case ActiveDays:
			current = stats.ActiveDays
		}
	}
	current = max(0, current)
	return ProgressResult{Current: current, Complete: challenge.Target > 0 && current >= challenge.Target}
}

func add(value, increment int64) int64 {
	if value > math.MaxInt64-increment {
		return math.MaxInt64
	}
	return value + increment
}
