package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	maxCommunityQRCodes       = 10
	maxCommunityQRCodeName    = 50
	maxCommunityQRCodeDesc    = 300
	maxCommunityQRCodeBytes   = 300 * 1024
	maxCommunityQRCodeDataLen = 420 * 1024
)

type CommunityQRCode struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ImageData   string     `json:"image_data"`
	Enabled     bool       `json:"enabled"`
	SortOrder   int        `json:"sort_order"`
	StartsAt    *time.Time `json:"starts_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

func (s *SettingService) GetCommunityQRCodes(ctx context.Context, activeOnly bool) ([]CommunityQRCode, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyCommunityQRCodes)
	if err != nil || strings.TrimSpace(raw) == "" {
		return []CommunityQRCode{}, nil
	}

	var items []CommunityQRCode
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("decode community QR codes: %w", err)
	}
	if !activeOnly {
		return items, nil
	}

	now := time.Now()
	active := make([]CommunityQRCode, 0, len(items))
	for _, item := range items {
		if !item.Enabled || (item.StartsAt != nil && now.Before(*item.StartsAt)) || (item.ExpiresAt != nil && !now.Before(*item.ExpiresAt)) {
			continue
		}
		active = append(active, item)
	}
	return active, nil
}

func (s *SettingService) SetCommunityQRCodes(ctx context.Context, items []CommunityQRCode) ([]CommunityQRCode, error) {
	if len(items) > maxCommunityQRCodes {
		return nil, fmt.Errorf("too many community QR codes (max %d)", maxCommunityQRCodes)
	}

	seen := make(map[string]struct{}, len(items))
	for i := range items {
		item := &items[i]
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		item.Description = strings.TrimSpace(item.Description)
		item.ImageData = strings.TrimSpace(item.ImageData)
		item.SortOrder = i

		if item.ID == "" {
			id, err := newCommunityQRCodeID()
			if err != nil {
				return nil, err
			}
			item.ID = id
		}
		if _, exists := seen[item.ID]; exists {
			return nil, fmt.Errorf("duplicate community QR code ID")
		}
		seen[item.ID] = struct{}{}
		if item.Name == "" || len([]rune(item.Name)) > maxCommunityQRCodeName {
			return nil, fmt.Errorf("community QR code name must be 1-%d characters", maxCommunityQRCodeName)
		}
		if len([]rune(item.Description)) > maxCommunityQRCodeDesc {
			return nil, fmt.Errorf("community QR code description is too long (max %d characters)", maxCommunityQRCodeDesc)
		}
		if err := validateCommunityQRCodeImage(item.ImageData); err != nil {
			return nil, err
		}
		if item.StartsAt != nil && item.ExpiresAt != nil && !item.ExpiresAt.After(*item.StartsAt) {
			return nil, fmt.Errorf("community QR code expiry must be after its start time")
		}
	}

	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("encode community QR codes: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyCommunityQRCodes, string(encoded)); err != nil {
		return nil, fmt.Errorf("save community QR codes: %w", err)
	}
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return items, nil
}

func validateCommunityQRCodeImage(value string) error {
	if value == "" {
		return fmt.Errorf("community QR code image is required")
	}
	if len(value) > maxCommunityQRCodeDataLen {
		return fmt.Errorf("community QR code image is too large")
	}

	prefixes := map[string][]byte{
		"data:image/png;base64,":  {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
		"data:image/jpeg;base64,": {0xff, 0xd8, 0xff},
		"data:image/webp;base64,": {'R', 'I', 'F', 'F'},
	}
	encoded := ""
	var signature []byte
	for prefix, expectedSignature := range prefixes {
		if strings.HasPrefix(value, prefix) {
			encoded = strings.TrimPrefix(value, prefix)
			signature = expectedSignature
			break
		}
	}
	if encoded == "" {
		return fmt.Errorf("community QR code image must be PNG, JPEG, or WebP")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("community QR code image is invalid")
	}
	if len(decoded) > maxCommunityQRCodeBytes {
		return fmt.Errorf("community QR code image exceeds %d KB", maxCommunityQRCodeBytes/1024)
	}
	if !bytes.HasPrefix(decoded, signature) {
		return fmt.Errorf("community QR code image content does not match its format")
	}
	if strings.HasPrefix(value, "data:image/webp") && (len(decoded) < 12 || string(decoded[8:12]) != "WEBP") {
		return fmt.Errorf("community QR code image content does not match its format")
	}
	return nil
}

func newCommunityQRCodeID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate community QR code ID: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
