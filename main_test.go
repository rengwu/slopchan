package main

import (
	"os"
	"path/filepath"
	"reflect"
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
