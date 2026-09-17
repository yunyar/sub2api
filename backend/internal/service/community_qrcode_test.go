//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type communityQRCodeSettingRepo struct {
	value string
}

func (r *communityQRCodeSettingRepo) Get(context.Context, string) (*Setting, error) {
	return nil, errors.New("not implemented")
}

func (r *communityQRCodeSettingRepo) GetValue(context.Context, string) (string, error) {
	if r.value == "" {
		return "", errors.New("not found")
	}
	return r.value, nil
}

func (r *communityQRCodeSettingRepo) Set(_ context.Context, _ string, value string) error {
	r.value = value
	return nil
}

func (r *communityQRCodeSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, errors.New("not implemented")
}

func (r *communityQRCodeSettingRepo) SetMultiple(context.Context, map[string]string) error {
	return errors.New("not implemented")
}

func (r *communityQRCodeSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return nil, errors.New("not implemented")
}

func (r *communityQRCodeSettingRepo) Delete(context.Context, string) error {
	return errors.New("not implemented")
}

func TestCommunityQRCodesRoundTripAndActiveFilter(t *testing.T) {
	repo := &communityQRCodeSettingRepo{}
	svc := NewSettingService(repo, nil)
	now := time.Now()
	image := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\ncontent"))

	saved, err := svc.SetCommunityQRCodes(context.Background(), []CommunityQRCode{
		{Name: "active", ImageData: image, Enabled: true},
		{Name: "disabled", ImageData: image, Enabled: false},
		{Name: "expired", ImageData: image, Enabled: true, ExpiresAt: communityQRTimePtr(now.Add(-time.Minute))},
	})
	require.NoError(t, err)
	require.Len(t, saved, 3)
	require.NotEmpty(t, saved[0].ID)

	active, err := svc.GetCommunityQRCodes(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, "active", active[0].Name)
}

func TestCommunityQRCodesRejectUnsafeImage(t *testing.T) {
	svc := NewSettingService(&communityQRCodeSettingRepo{}, nil)
	_, err := svc.SetCommunityQRCodes(context.Background(), []CommunityQRCode{
		{Name: "unsafe", ImageData: "data:image/svg+xml;base64,PHN2Zz4=", Enabled: true},
	})
	require.ErrorContains(t, err, "PNG, JPEG, or WebP")

	_, err = svc.SetCommunityQRCodes(context.Background(), []CommunityQRCode{
		{Name: "spoofed", ImageData: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not a png")), Enabled: true},
	})
	require.ErrorContains(t, err, "does not match")
}

func communityQRTimePtr(value time.Time) *time.Time {
	return &value
}
