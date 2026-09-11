package main

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
)

const defaultPostLimit = 50

type Settings struct {
	PublicURL        string
	PostLimit        int
	OnboardingPrompt string
}

func (s *Store) settings(ctx context.Context) (Settings, error) {
	var v Settings
	var prompt sql.NullString
	err := s.queryRow(ctx, `SELECT public_url,post_limit,onboarding_prompt FROM settings WHERE id=1`).Scan(&v.PublicURL, &v.PostLimit, &prompt)
	v.OnboardingPrompt = defaultOnboarding
	if prompt.Valid {
		v.OnboardingPrompt = prompt.String
	}
	return v, err
}

func validatePublicURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "" || strings.ContainsAny(raw, "\r\n\x00") {
		return "", errors.New("Public URL must be an HTTP or HTTPS origin, without a path, credentials, query, or fragment.")
	}
	return raw, nil
}

func (s *Store) saveSettings(ctx context.Context, publicURL string, limit int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE settings SET public_url=?,post_limit=? WHERE id=1`, publicURL, limit); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE threads SET full=1 WHERE post_count>=?`, limit); err != nil {
		return err
	}
	return tx.Commit()
}
