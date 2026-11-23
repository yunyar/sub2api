package bloomrules

import "time"

type Tier string

const (
	Easy   Tier = "easy"
	Medium Tier = "medium"
	Hard   Tier = "hard"
	Weekly Tier = "weekly"
)

type Metric string

const (
	CheckIn            Metric = "checkin"
	ProductiveRequests Metric = "productive_requests"
	OutputTokens       Metric = "output_tokens"
	Images             Metric = "images"
	ActiveDays         Metric = "active_days"
)

type Modality string

const (
	ColdStart Modality = "cold_start"
	Text      Modality = "text"
	Image     Modality = "image"
	Mixed     Modality = "mixed"
)

// Window includes Start and excludes End. Calendar constructs it in site time.
type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type Periods struct {
	Day           Window
	Week          Window
	PriorWeek     Window
	CompletedDays Window
}

type Usage struct {
	At           time.Time
	OutputTokens int64
	Images       int64
}

type Stats struct {
	Requests           int64
	ProductiveRequests int64
	OutputTokens       int64
	Images             int64
	ActiveDays         int64
}

// IssueInput uses aggregate snapshots over the corresponding Calendar windows.
// PriorWeekWalletSpendMicros is the floor of the aggregate actual wallet spend
// in micros. Sum the exact wallet charges before that conversion.
type IssueInput struct {
	AccountID                  int64
	At                         time.Time
	Location                   *time.Location
	PriorWeek                  Stats
	CompletedDays              Stats
	PriorWeekWalletSpendMicros int64
}

// Challenge is an issuance snapshot. Persist it and pass it back to Progress.
type Challenge struct {
	TemplateID   string `json:"template_id"`
	Tier         Tier   `json:"tier"`
	Metric       Metric `json:"metric"`
	Target       int64  `json:"target"`
	Window       Window `json:"window"`
	RewardMicros int64  `json:"reward_micros"`
}

type Assignment struct {
	WeeklyBudgetMicros int64
	Daily              [3]Challenge
	Weekly             Challenge
}

type Template struct {
	ID          string
	Tier        Tier
	Metric      Metric
	ImageMetric Metric
	ColdMetric  Metric
	MixedMetric Metric
	Intensity   int64
}

type ProgressResult struct {
	Current  int64
	Complete bool
}
