package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPostingTokens(t *testing.T) {
	t.Setenv("SLOPCHAN_TOKENS", "")
	path := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(path, []byte(" one, two ,\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tokens, err := postingTokens(path)
	if err != nil || !reflect.DeepEqual(tokens, []string{"one", "two"}) {
		t.Fatalf("tokens = %v, error = %v", tokens, err)
	}
	t.Setenv("SLOPCHAN_TOKENS", "environment-token")
	if _, err := postingTokens(path); err == nil {
		t.Fatal("ambiguous credential sources accepted")
	}
	tokens, err = postingTokens("")
	if err != nil || !reflect.DeepEqual(tokens, []string{"environment-token"}) {
		t.Fatalf("environment tokens = %v, error = %v", tokens, err)
	}
	t.Setenv("SLOPCHAN_TOKENS", "")
	if _, err := postingTokens(path + ".missing"); err == nil {
		t.Fatal("missing token file accepted")
	}
	if err := os.WriteFile(path, []byte(" , \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := postingTokens(path); err == nil {
		t.Fatal("empty token file accepted")
	}
}

func TestMissingTokensDoesNotCreateData(t *testing.T) {
	t.Setenv("SLOPCHAN_TOKENS", "")
	t.Setenv("SLOPCHAN_TOKEN_FILE", "")
	data := filepath.Join(t.TempDir(), "uncreated")
	if err := run([]string{"serve", "-data", data}); err == nil {
		t.Fatal("server accepted missing credentials")
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("invalid configuration created data: %v", err)
	}
}

func TestAdminCLIConfiguration(t *testing.T) {
	for _, key := range []string{"SLOPCHAN_TOKENS", "SLOPCHAN_TOKEN_FILE", "SLOPCHAN_ADMIN_EMAIL", "SLOPCHAN_ADMIN_PASSWORD", "SLOPCHAN_ADMIN_PASSWORD_FILE", "SLOPCHAN_TLS_CERT", "SLOPCHAN_TLS_KEY"} {
		t.Setenv(key, "")
	}
	data := filepath.Join(t.TempDir(), "data")
	for _, args := range [][]string{
		{"-admin-email", "owner@example.com"},
		{"-admin-email", "owner@example.com", "-admin-password", "short"},
		{"-tls-cert", "cert.pem"},
		{"-reset-admin"},
	} {
		if err := run(append([]string{"serve", "-data", data}, args...)); err == nil {
			t.Fatal("accepted incomplete configuration")
		}
		if _, err := os.Stat(data); !os.IsNotExist(err) {
			t.Fatal("invalid configuration created data")
		}
	}
	passwordFile := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(passwordFile, []byte("test-cli-password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// An invalid listen address stops after startup configuration, without running a server.
	err := run([]string{"serve", "-data", data, "-listen", "invalid-address", "-admin-email", "owner@example.com", "-admin-password-file", passwordFile})
	if err == nil {
		t.Fatal("expected invalid listener")
	}
	s, err := openStore(data)
	if err != nil {
		t.Fatal(err)
	}
	var email, hash string
	if err = s.db.QueryRow(`SELECT email,password_hash FROM admin`).Scan(&email, &hash); err != nil {
		t.Fatal(err)
	}
	if email != "owner@example.com" || !checkPassword(hash, "test-cli-password") {
		t.Fatal("CLI bootstrap failed")
	}
	s.db.Close()
	// Persisted configuration works with all bootstrap inputs removed.
	err = run([]string{"serve", "-data", data, "-listen", "invalid-address"})
	if err == nil || !strings.Contains(err.Error(), "missing port") {
		t.Fatalf("saved credentials not recognized: %v", err)
	}
}
