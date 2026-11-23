package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type bloomOpsTestRepository struct {
	OpsRepository
	event OpsAlertEvent
}

func (repo *bloomOpsTestRepository) GetAlertEventByID(_ context.Context, _ int64) (*OpsAlertEvent, error) {
	return &repo.event, nil
}

func (repo *bloomOpsTestRepository) UpdateAlertEventEmailSent(_ context.Context, _ int64, sent bool) error {
	repo.event.EmailSent = sent
	return nil
}

func TestBloomBudgetEmailToOpsRecipients(t *testing.T) {
	ctx := t.Context()
	settings := newNotificationEmailMemorySettingRepo()
	smtpServer := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, settings.SetMultiple(ctx, smtpServer.settings()))
	require.NoError(t, settings.Set(ctx, SettingKeyOpsEmailNotificationConfig,
		`{"alert":{"enabled":true,"recipients":["ops@example.com"],"min_severity":"warning"}}`))
	alerts := &bloomOpsTestRepository{event: OpsAlertEvent{ID: 8, Title: "任务奖励触线", Severity: "P0",
		Description: "本周配给 10，硬上限 11", Status: OpsAlertStatusFiring}}
	sender := NewOpsAlertEvaluatorService(&OpsService{settingRepo: settings}, alerts, NewEmailService(settings, nil), nil, nil, nil)
	sender.NotifyBloomBudgetAlert(ctx, alerts.event.ID)
	sender.NotifyBloomBudgetAlert(ctx, alerts.event.ID)
	require.True(t, alerts.event.EmailSent)
	require.EqualValues(t, 1, smtpServer.messageCount())
	require.Contains(t, smtpServer.lastMessage(), "ops@example.com")
}
