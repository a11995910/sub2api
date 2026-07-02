//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmailServiceGetSMTPConfigsIncludesFallbacks(t *testing.T) {
	repo := newMockSettingRepo()
	require.NoError(t, repo.SetMultiple(context.Background(), map[string]string{
		SettingKeySMTPHost:      "smtp-primary.example.com",
		SettingKeySMTPPort:      "465",
		SettingKeySMTPUsername:  "primary-user",
		SettingKeySMTPPassword:  "primary-pass",
		SettingKeySMTPFrom:      "primary@example.com",
		SettingKeySMTPFromName:  "Primary",
		SettingKeySMTPUseTLS:    "true",
		SettingKeySMTPFallbacks: `[{"host":"smtp-backup.example.com","port":587,"username":"backup-user","password":"backup-pass","from_email":"backup@example.com","from_name":"Backup","use_tls":false}]`,
	}))

	svc := NewEmailService(repo, nil)
	configs, err := svc.GetSMTPConfigs(context.Background())
	require.NoError(t, err)
	require.Len(t, configs, 2)
	require.Equal(t, "smtp-primary.example.com", configs[0].Host)
	require.Equal(t, 465, configs[0].Port)
	require.Equal(t, "primary-pass", configs[0].Password)
	require.True(t, configs[0].UseTLS)
	require.Equal(t, "smtp-backup.example.com", configs[1].Host)
	require.Equal(t, 587, configs[1].Port)
	require.Equal(t, "backup-pass", configs[1].Password)
	require.False(t, configs[1].UseTLS)
}

func TestEmailServiceSendEmailWithFallbackConfigsRetriesNextConfig(t *testing.T) {
	svc := NewEmailService(newMockSettingRepo(), nil)
	attemptedHosts := make([]string, 0, 2)
	svc.sendEmailWithConfigFunc = func(config *SMTPConfig, _, _, _ string) error {
		attemptedHosts = append(attemptedHosts, config.Host)
		if config.Host == "smtp-primary.example.com" {
			return errors.New("primary unavailable")
		}
		return nil
	}

	err := svc.SendEmailWithFallbackConfigsForPurpose([]*SMTPConfig{
		{Host: "smtp-primary.example.com", Port: 465},
		{Host: "smtp-backup.example.com", Port: 587},
	}, EmailPurposeAuthVerifyCode, "user@example.com", "subject", "body")

	require.NoError(t, err)
	require.Equal(t, []string{"smtp-primary.example.com", "smtp-backup.example.com"}, attemptedHosts)
}

func TestNormalizeSMTPFallbacksDropsEmptyHostAndDefaultsPort(t *testing.T) {
	fallbacks := NormalizeSMTPFallbacks([]SMTPFallbackConfig{
		{Host: " ", Port: 465, Username: "ignored"},
		{Host: " smtp-backup.example.com ", Username: " user ", Password: " pass ", From: " from@example.com ", FromName: " Backup "},
	})

	require.Len(t, fallbacks, 1)
	require.Equal(t, "smtp-backup.example.com", fallbacks[0].Host)
	require.Equal(t, 587, fallbacks[0].Port)
	require.Equal(t, "user", fallbacks[0].Username)
	require.Equal(t, "pass", fallbacks[0].Password)
	require.Equal(t, "from@example.com", fallbacks[0].From)
	require.Equal(t, "Backup", fallbacks[0].FromName)
}
