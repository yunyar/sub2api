//go:build integration

package repository

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/bloomrules"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestBloomFirstVisitFreezesAndPaysOneWeek(t *testing.T) {
	ctx := t.Context()
	owner := mustCreateUser(t, integrationEntClient, &service.User{Balance: 2})
	repo := NewBloomRepository(integrationDB)
	firstVisit := time.Date(2026, 11, 26, 16, 0, 0, 0, time.UTC)
	start := bloomrules.Calendar(firstVisit, time.UTC).Week.Start
	schedule := bloomrules.IssueWeek(bloomrules.IssueInput{AccountID: owner.ID, At: firstVisit})
	var challenges []bloomrules.Challenge
	for _, day := range schedule {
		challenges = append(challenges, day.Daily[:]...)
	}
	challenges = append(challenges, schedule[0].Weekly)
	buildDrafts := func(generation int) []BloomTaskDraft {
		drafts := make([]BloomTaskDraft, 0, len(challenges))
		for _, challenge := range challenges {
			template, err := json.Marshal(struct {
				Generation int `json:"generation"`
				bloomrules.Challenge
			}{generation, challenge})
			require.NoError(t, err)
			drafts = append(drafts, BloomTaskDraft{PeriodStart: challenge.Window.Start,
				Difficulty: BloomDifficulty(challenge.Tier), Template: template, RewardMicros: challenge.RewardMicros})
		}
		return drafts
	}
	var workers sync.WaitGroup
	const attempts = 5
	errors := make(chan error, attempts)
	ready := make(chan struct{})
	for generation := range attempts {
		drafts := buildDrafts(generation)
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-ready
			_, err := repo.EnsureTasks(ctx, owner.ID, start, 10_000_000, drafts)
			errors <- err
		}()
	}
	close(ready)
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	week, err := repo.GetWeek(ctx, owner.ID, start)
	require.NoError(t, err)
	require.Len(t, week.Tasks, 22)
	generations := map[int]bool{}
	for _, task := range week.Tasks {
		var template struct {
			Generation int `json:"generation"`
		}
		require.NoError(t, json.Unmarshal(task.Template, &template))
		generations[template.Generation] = true
		claim, err := repo.ClaimTask(ctx, owner.ID, task.ID, time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.True(t, claim.Applied)
	}
	require.Len(t, generations, 1)
	week, err = repo.GetWeek(ctx, owner.ID, start)
	require.NoError(t, err)
	require.EqualValues(t, 10_000_000, week.SpentMicros)
	var balance decimal.Decimal
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=$1`, owner.ID).Scan(&balance))
	require.Equal(t, "12", balance.String())
	nextWeek, err := repo.GetWeek(ctx, owner.ID, start.AddDate(0, 0, 7))
	require.NoError(t, err)
	require.False(t, nextWeek.Exists)
}
