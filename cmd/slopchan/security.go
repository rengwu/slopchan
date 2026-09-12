package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const passwordIterations = 600000

func randomSecret() string { return hex.EncodeToString(randomBytes(32)) }
func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
func secretHash(secret string) []byte { h := sha256.Sum256([]byte(secret)); return h[:] }

// PBKDF2-HMAC-SHA256 uses an independent salt per password. Passwords are never persisted.
func hashPassword(password string) (string, error) {
	salt := randomBytes(16)
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		return "", err
	}
	return "pbkdf2-sha256$600000$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}
func checkPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" || len(password) > 1024 {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[2])
	expected, e2 := base64.RawStdEncoding.DecodeString(parts[3])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(expected) != 32 {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	return err == nil && subtle.ConstantTimeCompare(key, expected) == 1
}
func validateCredentials(email, password string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 {
		return "", errors.New("Enter a valid email address.")
	}
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return "", errors.New("Password must contain at least 12 characters and at most 1,024 bytes.")
	}
	return email, nil
}

// Startup credentials bootstrap the account; saved changes survive restarts.
func (s *Store) bootstrapAdmin(ctx context.Context, email, password string, reset bool) error {
	if email == "" && password == "" {
		if reset {
			return errors.New("admin reset requires email and password")
		}
		return nil
	}
	email, err := validateCredentials(email, password)
	if err != nil {
		return err
	}
	var exists int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin`).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 && !reset {
		return nil
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO admin(id,email,password_hash) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET email=excluded.email,password_hash=excluded.password_hash`, email, hash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
		return err
	}
	return tx.Commit()
}

// The key is separate from SQLite, owner-readable only, and must travel with backups.
func tokenCipher(dir string, db *sql.DB) (cipher.AEAD, error) {
	conn, err := db.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	path := filepath.Join(dir, "token.key")
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		var count int
		if err = conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM access_tokens WHERE length(secret)>0`).Scan(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, errors.New("token.key is missing; restore it from the same backup as the database")
		}
		key = randomBytes(32)
		f, e := os.CreateTemp(dir, ".token-key-*")
		if e != nil {
			return nil, e
		}
		defer os.Remove(f.Name())
		_, err = f.Write(key)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(f.Name(), path)
		}
		if err == nil {
			err = syncImageDirectory(dir)
		}
	}
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid token encryption key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (a *App) saveToken(ctx context.Context, name, token string) error {
	hash := secretHash(token)
	nonce := randomBytes(a.cipher.NonceSize())
	encrypted := a.cipher.Seal(nonce, nonce, []byte(token), hash)
	_, err := a.store.db.ExecContext(ctx, `INSERT INTO access_tokens(name,hash,secret,created_at) VALUES(?,?,?,?) ON CONFLICT(hash) DO NOTHING`, name, hash, encrypted, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (a *App) validToken(ctx context.Context, token string) (bool, error) {
	result, err := a.store.db.ExecContext(ctx, `UPDATE access_tokens SET last_used_at=? WHERE hash=? AND revoked_at=''`, time.Now().UTC().Format(time.RFC3339Nano), secretHash(token))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
func (a *App) decryptToken(ctx context.Context, id int64) (string, error) {
	var hash, encrypted []byte
	err := a.store.db.QueryRowContext(ctx, `SELECT hash,secret FROM access_tokens WHERE id=? AND revoked_at=''`, id).Scan(&hash, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	if err != nil {
		return "", err
	}
	n := a.cipher.NonceSize()
	if len(encrypted) < n {
		return "", errors.New("invalid encrypted token")
	}
	plain, err := a.cipher.Open(nil, encrypted[:n], encrypted[n:], hash)
	if err != nil {
		return "", fmt.Errorf("decrypt token: %w", err)
	}
	return string(plain), nil
}
