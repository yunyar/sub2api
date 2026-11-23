package bloomrules

import (
	"math"
	"math/big"
	"testing"
	"time"
)

func TestBudgetAndRewards(t *testing.T) {
	for _, test := range []struct {
		requests, spend, want int64
	}{
		{0, 0, 10_000_000},
		{0, 1_500_000, 10_000_000},
		{1, 0, 0},
		{1, -1, 0},
		{1, 9, 0},
		{1, 1_234_569, 123_456},
	} {
		if got := WeeklyBudgetMicros(test.requests, test.spend); got != test.want {
			t.Errorf("WeeklyBudgetMicros(%d, %d) = %d, want %d", test.requests, test.spend, got, test.want)
		}
	}
	budgets := []int64{0, 1, 3, 9, 10, 99, 100, 101, 123_456, DefaultWeeklyBudgetMicros, math.MaxInt64}
	for _, budget := range budgets {
		var dailyTotal int64
		for _, rule := range []struct {
			tier Tier
			rate int64
		}{
			{Easy, 1}, {Medium, 3}, {Hard, 6}, {Weekly, 30},
		} {
			exact := new(big.Int).Mul(big.NewInt(budget), big.NewInt(rule.rate))
			exact.Quo(exact, big.NewInt(100))
			got := RewardMicros(rule.tier, budget)
			if got != exact.Int64() {
				t.Fatalf("budget %d, %s reward = %d, want %s", budget, rule.tier, got, exact)
			}
			if rule.tier != Weekly {
				dailyTotal += got
			}
		}
		if total := dailyTotal*7 + RewardMicros(Weekly, budget); total > budget {
			t.Errorf("total rewards %d exceed budget %d", total, budget)
		}
	}
}

func TestTemplatePoolsRotateAndAreExecutable(t *testing.T) {
	histories := []Stats{
		{},
		{Requests: 20},
		{Requests: 20, ProductiveRequests: 18, OutputTokens: 2_000, ActiveDays: 5},
		{Requests: 20, ProductiveRequests: 18, Images: 40, ActiveDays: 5},
		{Requests: 20, ProductiveRequests: 18, OutputTokens: 2_000, Images: 40, ActiveDays: 5},
	}
	start := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	accountSchedulesDiffer := false
	checkIns := 0
	for tierIndex, tier := range [...]Tier{Easy, Medium, Hard, Weekly} {
		catalog := Templates(tier)
		if len(catalog) != 50 {
			t.Fatalf("%s has %d templates, want 50", tier, len(catalog))
		}
		seen := make(map[string]bool)
		for period := range catalog {
			days := period
			if tier == Weekly {
				days *= 7
			}
			input := IssueInput{AccountID: 44, At: start.AddDate(0, 0, days)}
			first := tierChallenge(Issue(input), tierIndex)
			repeat := tierChallenge(Issue(input), tierIndex)
			if seen[first.TemplateID] || first != repeat {
				t.Fatalf("%s period %d repeated a template or changed its issuance: %+v", tier, period, first)
			}
			seen[first.TemplateID] = true
			if first.Metric == CheckIn {
				checkIns++
			}
			input.AccountID = 45
			accountSchedulesDiffer = accountSchedulesDiffer || tierChallenge(Issue(input), tierIndex).TemplateID != first.TemplateID
			input.AccountID = 44
			for _, history := range histories {
				input.PriorWeek, input.CompletedDays = history, history
				challenge := tierChallenge(Issue(input), tierIndex)
				assertExecutable(t, challenge, tier, Classify(history))
			}
		}
		for _, template := range catalog {
			if !seen[template.ID] {
				t.Errorf("%s did not rotate through %s", tier, template.ID)
			}
		}
	}
	if !accountSchedulesDiffer || checkIns != 1 {
		t.Errorf("accounts differ = %v, check-in templates = %d", accountSchedulesDiffer, checkIns)
	}
}

func tierChallenge(assignment Assignment, index int) Challenge {
	if index == len(assignment.Daily) {
		return assignment.Weekly
	}
	return assignment.Daily[index]
}

func assertExecutable(t *testing.T, challenge Challenge, tier Tier, modality Modality) {
	t.Helper()
	if challenge.Tier != tier || challenge.Target < 1 {
		t.Fatalf("invalid challenge: %+v", challenge)
	}
	if challenge.Metric == CheckIn {
		if tier != Easy || !Progress(challenge, Stats{}, true).Complete {
			t.Fatalf("invalid check-in: %+v", challenge)
		}
		return
	}
	if got := Progress(challenge, Stats{Requests: 100}, true); got != (ProgressResult{}) {
		t.Fatalf("unproductive activity counted: %+v", got)
	}
	stats := Stats{ProductiveRequests: 1}
	switch challenge.Metric {
	case ProductiveRequests:
		stats.ProductiveRequests = challenge.Target
	case OutputTokens:
		if modality != Text && modality != Mixed {
			t.Fatalf("%s got a token target", modality)
		}
		stats.OutputTokens = challenge.Target
	case Images:
		if modality != Image && modality != Mixed {
			t.Fatalf("%s got an image target", modality)
		}
		stats.Images = challenge.Target
	case ActiveDays:
		if tier != Weekly || challenge.Target > 7 {
			t.Fatalf("unattainable active-day target: %+v", challenge)
		}
		stats.ProductiveRequests = challenge.Target
		stats.ActiveDays = challenge.Target
	default:
		t.Fatalf("unknown metric: %s", challenge.Metric)
	}
	if got := Progress(challenge, stats, false); !got.Complete || got.Current != challenge.Target {
		t.Fatalf("challenge %+v got progress %+v at its target", challenge, got)
	}
}

func TestIssueUsesCompletedDaysAndPersistedTarget(t *testing.T) {
	monday := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	heavy := Stats{Requests: 700, ProductiveRequests: 700, OutputTokens: 700_000, ActiveDays: 7}
	input := IssueInput{AccountID: 1, At: monday, PriorWeek: heavy, PriorWeekWalletSpendMicros: 20_000_000}
	opened := Issue(input)
	input.CompletedDays = Stats{Requests: 10_000, ProductiveRequests: 10_000, Images: 10_000, ActiveDays: 1}
	if got := Issue(input); got != opened {
		t.Fatal("Monday issuance used an incomplete day")
	}
	input.At = monday.AddDate(0, 0, 2)
	input.CompletedDays = Stats{Requests: 600}
	quiet := Issue(input)
	input.CompletedDays = heavy
	busy := Issue(input)
	for index := 1; index < len(quiet.Daily); index++ {
		before, after := quiet.Daily[index], busy.Daily[index]
		if before.TemplateID != after.TemplateID || before.Metric != after.Metric || before.Target >= after.Target {
			t.Fatalf("completed-day output did not increase difficulty: %+v, %+v", before, after)
		}
		progress := Stats{Requests: 20, ProductiveRequests: before.Target, OutputTokens: before.Target}
		if !Progress(before, progress, false).Complete || Progress(after, progress, false).Complete {
			t.Fatal("issued target was not preserved when evaluating progress")
		}
	}
	if quiet.Weekly != opened.Weekly || busy.Weekly != opened.Weekly {
		t.Fatal("weekly challenge changed with this week's activity")
	}
	if Progress(Challenge{Tier: Medium, Metric: CheckIn, Target: 1}, Stats{}, true).Complete {
		t.Fatal("non-easy check-in was accepted")
	}
}
