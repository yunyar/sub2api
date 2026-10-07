//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type authIPRiskAssociation struct {
	userID int64
	ip     string
	source string
}

type authIPRiskServiceStub struct {
	checkErr  error
	checkIPs  []string
	recordErr error
	records   []authIPRiskAssociation
}

func (s *authIPRiskServiceStub) CheckIP(_ context.Context, rawIP string) error {
	s.checkIPs = append(s.checkIPs, rawIP)
	return s.checkErr
}

func (s *authIPRiskServiceStub) RecordUserIP(_ context.Context, userID int64, rawIP, source string) error {
	s.records = append(s.records, authIPRiskAssociation{userID: userID, ip: rawIP, source: source})
	return s.recordErr
}

func authRiskTestContext() context.Context {
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "8.8.8.8"})
	return WithPaymentRiskIP(ctx, "8.8.8.8")
}

func TestPaymentRiskIPDistinguishesUnsetFromResolvedEmpty(t *testing.T) {
	binding := &SessionBinding{IP: "8.8.8.8"}
	unsetContext := WithSessionBinding(context.Background(), binding)
	emptyContext := WithPaymentRiskIP(unsetContext, "")

	_, set := PaymentRiskIPFromContext(unsetContext)
	require.False(t, set)
	require.Empty(t, trustedPublicSessionIP(unsetContext))

	rawIP, set := PaymentRiskIPFromContext(emptyContext)
	require.True(t, set)
	require.Empty(t, rawIP)
	require.Empty(t, trustedPublicSessionIP(emptyContext))
}

func TestAuthServiceRegisterRejectsBlockedTrustedIPBeforeCreatingUser(t *testing.T) {
	repo := &userRepoStub{nextID: 42}
	svc := newAuthService(repo, map[string]string{
		SettingKeyRegistrationEnabled: "true",
	}, nil, nil)
	risk := &authIPRiskServiceStub{checkErr: ErrPaymentRiskIPBlocked}
	svc.SetPaymentRiskService(risk)

	_, _, err := svc.Register(authRiskTestContext(), "new-user@example.com", "password")

	require.ErrorIs(t, err, ErrPaymentRiskIPBlocked)
	require.Equal(t, []string{"8.8.8.8"}, risk.checkIPs)
	require.Empty(t, repo.created)
	require.Empty(t, risk.records)
}

func TestAuthServiceRegisterRecordsIPOnlyAfterSuccessfulRegistration(t *testing.T) {
	repo := &userRepoStub{nextID: 43}
	svc := newAuthService(repo, map[string]string{
		SettingKeyRegistrationEnabled: "true",
	}, nil, nil)
	risk := &authIPRiskServiceStub{}
	svc.SetPaymentRiskService(risk)

	_, user, err := svc.Register(authRiskTestContext(), "new-user@example.com", "password")

	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, []string{"8.8.8.8"}, risk.checkIPs)
	require.Equal(t, []authIPRiskAssociation{{userID: user.ID, ip: "8.8.8.8", source: "registration"}}, risk.records)
}

func TestOAuthRegistrationChecksRiskOnlyForNewAccounts(t *testing.T) {
	ctx := authRiskTestContext()
	blockedRisk := &authIPRiskServiceStub{checkErr: ErrPaymentRiskIPBlocked}
	newUserRepo := &userRepoStub{}
	newUserService := newAuthService(newUserRepo, map[string]string{
		SettingKeyRegistrationEnabled: "true",
	}, nil, nil)
	newUserService.refreshTokenCache = &refreshTokenCacheStub{}
	newUserService.SetPaymentRiskService(blockedRisk)

	_, _, err := newUserService.LoginOrRegisterOAuthWithTokenPair(ctx, "fresh@example.com", "fresh", "", "", "oidc")

	require.ErrorIs(t, err, ErrPaymentRiskIPBlocked)
	require.Empty(t, newUserRepo.created)
	require.Equal(t, []string{"8.8.8.8"}, blockedRisk.checkIPs)

	existing := &User{
		ID:      44,
		Email:   "existing@example.com",
		Role:    RoleUser,
		Status:  StatusActive,
		Balance: 1,
	}
	existingRisk := &authIPRiskServiceStub{checkErr: ErrPaymentRiskIPBlocked}
	existingUserService := newAuthService(&userRepoStub{user: existing}, map[string]string{
		SettingKeyRegistrationEnabled: "true",
	}, nil, nil)
	existingUserService.refreshTokenCache = &refreshTokenCacheStub{}
	existingUserService.SetPaymentRiskService(existingRisk)

	_, user, err := existingUserService.LoginOrRegisterOAuthWithTokenPair(ctx, existing.Email, "existing", "", "", "oidc")

	require.NoError(t, err)
	require.Equal(t, existing.ID, user.ID)
	require.Empty(t, existingRisk.checkIPs)
}

func TestRecordSuccessfulLoginAssociatesOnlyVerifiedActiveUser(t *testing.T) {
	user := &User{ID: 45, Email: "verified@example.com", Role: RoleUser, Status: StatusActive}
	repo := &userRepoStub{user: user}
	svc := newAuthService(repo, nil, nil, nil)
	risk := &authIPRiskServiceStub{}
	svc.SetPaymentRiskService(risk)

	require.NoError(t, svc.RecordSuccessfulLogin(authRiskTestContext(), user.ID))

	require.Equal(t, []authIPRiskAssociation{{userID: user.ID, ip: "8.8.8.8", source: "login"}}, risk.records)
	require.Empty(t, risk.checkIPs)
}

func TestFailedLoginDoesNotAssociateIP(t *testing.T) {
	svc := newAuthService(&userRepoStub{}, nil, nil, nil)
	risk := &authIPRiskServiceStub{}
	svc.SetPaymentRiskService(risk)

	_, err := svc.ValidatePasswordCredentials(authRiskTestContext(), "unverified@example.com", "wrong-password")

	require.Error(t, err)
	require.Empty(t, risk.records)
	require.Empty(t, risk.checkIPs)
}

func TestRegistrationFailsClosedWhenIPEvidenceCannotBeStored(t *testing.T) {
	repo := &userRepoStub{nextID: 46}
	svc := newAuthService(repo, map[string]string{
		SettingKeyRegistrationEnabled: "true",
	}, nil, nil)
	risk := &authIPRiskServiceStub{recordErr: errors.New("risk store unavailable")}
	svc.SetPaymentRiskService(risk)

	token, _, err := svc.Register(authRiskTestContext(), "record-failure@example.com", "password")

	require.ErrorContains(t, err, "risk store unavailable")
	require.Empty(t, token)
	require.Len(t, repo.created, 1)
	require.Equal(t, []authIPRiskAssociation{{userID: 46, ip: "8.8.8.8", source: "registration"}}, risk.records)
}

func TestPasswordLoginFailsClosedBeforeTokenWhenIPEvidenceCannotBeStored(t *testing.T) {
	svc := newAuthService(&userRepoStub{}, nil, nil, nil)
	passwordHash, err := svc.HashPassword("correct-password")
	require.NoError(t, err)
	user := &User{
		ID:           47,
		Email:        "known@example.com",
		PasswordHash: passwordHash,
		Role:         RoleUser,
		Status:       StatusActive,
	}
	repo := &userRepoStub{user: user}
	svc.userRepo = repo
	risk := &authIPRiskServiceStub{recordErr: errors.New("risk store unavailable")}
	svc.SetPaymentRiskService(risk)

	token, _, err := svc.Login(authRiskTestContext(), user.Email, "correct-password")

	require.ErrorContains(t, err, "risk store unavailable")
	require.Empty(t, token)
	require.Equal(t, []authIPRiskAssociation{{userID: user.ID, ip: "8.8.8.8", source: "login"}}, risk.records)
}

func TestPrivateIPSkipsRiskChecksAndAssociations(t *testing.T) {
	repo := &userRepoStub{nextID: 48}
	svc := newAuthService(repo, map[string]string{
		SettingKeyRegistrationEnabled: "true",
	}, nil, nil)
	risk := &authIPRiskServiceStub{
		checkErr:  ErrPaymentRiskIPBlocked,
		recordErr: errors.New("risk store unavailable"),
	}
	svc.SetPaymentRiskService(risk)
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "10.1.2.3"})

	token, user, err := svc.Register(ctx, "private-ip@example.com", "password")

	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.NotNil(t, user)
	require.Empty(t, risk.checkIPs)
	require.Empty(t, risk.records)
}
