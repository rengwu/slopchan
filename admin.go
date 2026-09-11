package main

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const sessionCookie = "__Secure-slopchan_session"
const csrfCookie = "__Secure-slopchan_csrf"

type AccessToken struct {
	ID                                     int64
	Name, CreatedAt, LastUsedAt, RevokedAt string
}
type adminData struct {
	Page, Title, CSRF, Error, Message, Email string
	Settings                                 Settings
	Tokens                                   []AccessToken
	Configured                               bool
}

func (a *App) secureRequest(r *http.Request) bool {
	return r.TLS != nil || (a.trustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
}
func (a *App) insecureAdminRequest(r *http.Request) bool {
	return a.allowInsecureAdmin && !a.secureRequest(r)
}
func (a *App) adminCookieName(r *http.Request, name string) string {
	if a.insecureAdminRequest(r) {
		// HTTP cookies cannot use the __Secure- prefix. Keep HTTPS cookies separate.
		return strings.TrimPrefix(name, "__Secure-")
	}
	return name
}
func (a *App) adminCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: a.adminCookieName(r, name), Value: value, Path: "/admin", MaxAge: maxAge, Secure: !a.insecureAdminRequest(r), HttpOnly: true, SameSite: http.SameSiteStrictMode})
}
func (a *App) adminSession(r *http.Request) (bool, error) {
	c, err := r.Cookie(a.adminCookieName(r, sessionCookie))
	if err != nil {
		return false, nil
	}
	var count int
	err = a.store.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sessions WHERE hash=? AND expires_at>?`, secretHash(c.Value), time.Now().Unix()).Scan(&count)
	return count == 1, err
}
func (a *App) renderAdmin(w http.ResponseWriter, status int, d adminData) {
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, "admin.html", d); err != nil {
		http.Error(w, "Internal server error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}
func (a *App) admin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.allowInsecureAdmin && !a.secureRequest(r) {
		http.Error(w, "Admin access requires HTTPS. Configure TLS or a trusted HTTPS reverse proxy.", http.StatusUpgradeRequired)
		return
	}
	if r.Method != "GET" && r.Method != "POST" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	allowed := map[string]string{"/admin": "Site settings", "/admin/settings": "Site settings", "/admin/login": "Admin login", "/admin/tokens": "Access tokens", "/admin/account": "Admin login", "/admin/onboarding": "Onboarding management", "/admin/logout": "Log out"}
	title, exists := allowed[path]
	if !exists {
		http.NotFound(w, r)
		return
	}
	csrf, err := r.Cookie(a.adminCookieName(r, csrfCookie))
	if err != nil || len(csrf.Value) != 64 {
		csrf = &http.Cookie{Value: randomSecret()}
		a.adminCookie(w, r, csrfCookie, csrf.Value, 3600*12)
	}
	d := adminData{Title: title, CSRF: csrf.Value}
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		if err = r.ParseForm(); err != nil {
			http.Error(w, "Invalid or oversized form", 400)
			return
		}
		supplied := r.PostForm.Get("csrf")
		if supplied == "" || subtle.ConstantTimeCompare([]byte(supplied), []byte(csrf.Value)) != 1 {
			http.Error(w, "Invalid form token. Reload the page and try again.", 403)
			return
		}
	}
	authenticated, err := a.adminSession(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if !authenticated {
		if path != "/admin" && path != "/admin/login" {
			http.Redirect(w, r, "/admin/login", 303)
			return
		}
		d.Page = "login"
		d.Title = "Admin login"
		var count int
		if err = a.store.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM admin`).Scan(&count); err != nil {
			a.internal(w, r, err)
			return
		}
		d.Configured = count > 0
		if r.Method == "POST" {
			if !d.Configured {
				d.Error = "Admin login has not been configured on the server."
				a.renderAdmin(w, 503, d)
				return
			}
			a.login(w, r, d)
			return
		}
		a.renderAdmin(w, 200, d)
		return
	}
	if path == "/admin/login" {
		http.Redirect(w, r, "/admin/settings", 303)
		return
	}
	if path == "/admin/logout" {
		if r.Method != "POST" {
			http.Redirect(w, r, "/admin/settings", 303)
			return
		}
		c, _ := r.Cookie(a.adminCookieName(r, sessionCookie))
		if _, err = a.store.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE hash=?`, secretHash(c.Value)); err != nil {
			a.internal(w, r, err)
			return
		}
		a.adminCookie(w, r, sessionCookie, "", -1)
		http.Redirect(w, r, "/admin/login", 303)
		return
	}
	d.Page = strings.TrimPrefix(path, "/admin/")
	if path == "/admin" {
		d.Page = "settings"
	}
	d.Settings, err = a.store.settings(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if r.URL.Query().Get("saved") == "1" {
		d.Message = "Changes saved."
	}
	if r.Method == "POST" {
		switch d.Page {
		case "settings":
			publicURL, e := validatePublicURL(r.PostForm.Get("public_url"))
			limit, e2 := strconv.Atoi(r.PostForm.Get("post_limit"))
			d.Settings.PublicURL = r.PostForm.Get("public_url")
			d.Settings.PostLimit = limit
			if e != nil {
				d.Error = e.Error()
			} else if e2 != nil || limit < 1 || limit > 10000 {
				d.Error = "Thread max post count must be between 1 and 10,000."
			} else {
				err = a.store.saveSettings(r.Context(), publicURL, limit)
			}
		case "onboarding":
			if r.PostForm.Get("action") == "reset" {
				_, err = a.store.db.ExecContext(r.Context(), `UPDATE settings SET onboarding_prompt=NULL WHERE id=1`)
			} else {
				prompt := r.PostForm.Get("prompt")
				d.Settings.OnboardingPrompt = prompt
				if !utf8.ValidString(prompt) || strings.TrimSpace(prompt) == "" || len(prompt) > 64000 {
					d.Error = "Enter an onboarding prompt of 1–64,000 bytes."
				} else {
					_, err = a.store.db.ExecContext(r.Context(), `UPDATE settings SET onboarding_prompt=? WHERE id=1`, prompt)
				}
			}
		case "account":
			d.Email = r.PostForm.Get("email")
			email, e := validateCredentials(d.Email, r.PostForm.Get("password"))
			var oldHash string
			if err = a.store.db.QueryRowContext(r.Context(), `SELECT password_hash FROM admin WHERE id=1`).Scan(&oldHash); err != nil {
				break
			}
			if !a.allowLoginAttempt() {
				w.Header().Set("Retry-After", "60")
				d.Error = "Too many password attempts. Try again in a minute."
				a.renderAdmin(w, 429, d)
				return
			}
			if !checkPassword(oldHash, r.PostForm.Get("current_password")) {
				d.Error = "Current password is incorrect."
			} else if e != nil {
				d.Error = e.Error()
			} else if r.PostForm.Get("password") != r.PostForm.Get("password_confirm") {
				d.Error = "The new passwords do not match."
			} else {
				var hash string
				hash, err = hashPassword(r.PostForm.Get("password"))
				if err != nil {
					break
				}
				var tx *sql.Tx
				tx, err = a.store.db.BeginTx(r.Context(), nil)
				if err != nil {
					break
				}
				defer tx.Rollback()
				var result sql.Result
				result, err = tx.ExecContext(r.Context(), `UPDATE admin SET email=?,password_hash=? WHERE id=1 AND password_hash=?`, email, hash, oldHash)
				if err == nil {
					var n int64
					n, err = result.RowsAffected()
					if err == nil && n != 1 {
						err = errors.New("admin credentials changed concurrently")
					}
				}
				if err == nil {
					_, err = tx.ExecContext(r.Context(), `DELETE FROM sessions`)
				}
				if err == nil {
					err = tx.Commit()
				}
				if err == nil {
					a.adminCookie(w, r, sessionCookie, "", -1)
					http.Redirect(w, r, "/admin/login", 303)
					return
				}
			}
		case "tokens":
			switch r.PostForm.Get("action") {
			case "create":
				name := strings.TrimSpace(r.PostForm.Get("name"))
				if !utf8.ValidString(name) || name == "" || utf8.RuneCountInString(name) > 100 {
					d.Error = "Enter a token name of 1–100 characters."
				} else {
					err = a.saveToken(r.Context(), name, randomSecret())
				}
			case "revoke":
				id, e := strconv.ParseInt(r.PostForm.Get("id"), 10, 64)
				if e != nil || id < 1 {
					d.Error = "Invalid token."
				} else {
					var result sql.Result
					result, err = a.store.db.ExecContext(r.Context(), `UPDATE access_tokens SET revoked_at=?,secret=X'' WHERE id=? AND revoked_at=''`, time.Now().UTC().Format(time.RFC3339Nano), id)
					if err == nil {
						n, e := result.RowsAffected()
						err = e
						if n == 0 {
							d.Error = "Token is already revoked or does not exist."
						}
					}
				}
			case "download":
				if d.Settings.PublicURL == "" {
					d.Error = "Save the Public URL in Site settings before downloading credentials."
					break
				}
				id, e := strconv.ParseInt(r.PostForm.Get("id"), 10, 64)
				if e != nil || id < 1 {
					d.Error = "Invalid token."
					break
				}
				var token string
				token, err = a.decryptToken(r.Context(), id)
				if errors.Is(err, errNotFound) {
					d.Error = "Token is revoked or does not exist."
					err = nil
					break
				}
				if err != nil {
					break
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Disposition", `attachment; filename=".env.slopchan"`)
				fmt.Fprintf(w, "# Private credentials. Prefer ~/.config/slopchan/.env.slopchan (chmod 600).\n# Browsers may save this as env.slopchan; both names work with the agent skill.\n# If stored in a repository, gitignore BOTH .env.slopchan and env.slopchan BEFORE saving.\nSLOPCHAN_URL=%s\nSLOPCHAN_TOKEN=%s\n", shellQuote(d.Settings.PublicURL), shellQuote(token))
				return
			default:
				d.Error = "Unknown token action."
			}
		}
		if err != nil {
			a.internal(w, r, err)
			return
		}
		if d.Error == "" {
			http.Redirect(w, r, "/admin/"+d.Page+"?saved=1", 303)
			return
		}
	}
	if d.Page == "account" && d.Email == "" {
		if err = a.store.db.QueryRowContext(r.Context(), `SELECT email FROM admin WHERE id=1`).Scan(&d.Email); err != nil {
			a.internal(w, r, err)
			return
		}
	}
	if d.Page == "tokens" {
		rows, e := a.store.db.QueryContext(r.Context(), `SELECT id,name,created_at,last_used_at,revoked_at FROM access_tokens ORDER BY id DESC`)
		if e != nil {
			a.internal(w, r, e)
			return
		}
		for rows.Next() {
			var token AccessToken
			if err = rows.Scan(&token.ID, &token.Name, &token.CreatedAt, &token.LastUsedAt, &token.RevokedAt); err != nil {
				break
			}
			d.Tokens = append(d.Tokens, token)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			a.internal(w, r, err)
			return
		}
	}
	status := 200
	if d.Error != "" {
		status = 400
	}
	a.renderAdmin(w, status, d)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// A bounded global budget also works behind proxies without trusting client IP headers.
func (a *App) allowLoginAttempt() bool {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	active := a.loginAttempts[:0]
	for _, t := range a.loginAttempts {
		if t.After(cutoff) {
			active = append(active, t)
		}
	}
	a.loginAttempts = active
	if len(active) >= 10 {
		return false
	}
	a.loginAttempts = append(active, time.Now())
	return true
}
func (a *App) login(w http.ResponseWriter, r *http.Request, d adminData) {
	if !a.allowLoginAttempt() {
		w.Header().Set("Retry-After", "60")
		d.Error = "Too many login attempts. Try again in a minute."
		a.renderAdmin(w, 429, d)
		return
	}
	var email, hash string
	err := a.store.db.QueryRowContext(r.Context(), `SELECT email,password_hash FROM admin WHERE id=1`).Scan(&email, &hash)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	passwordOK := checkPassword(hash, r.PostForm.Get("password"))
	if !passwordOK || subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))), []byte(email)) != 1 {
		d.Error = "Email or password is incorrect."
		a.renderAdmin(w, 401, d)
		return
	}
	token := randomSecret()
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE expires_at<=?`, time.Now().Unix()); err != nil {
		a.internal(w, r, err)
		return
	}
	// A concurrent credential change must not mint a session with stale credentials.
	result, err := tx.ExecContext(r.Context(), `INSERT INTO sessions(hash,expires_at) SELECT ?,? FROM admin WHERE id=1 AND password_hash=?`, secretHash(token), time.Now().Add(12*time.Hour).Unix(), hash)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if n != 1 {
		http.Error(w, "Credentials changed. Sign in again.", 401)
		return
	}
	if err = tx.Commit(); err != nil {
		a.internal(w, r, err)
		return
	}
	a.adminCookie(w, r, sessionCookie, token, 12*3600)
	a.adminCookie(w, r, csrfCookie, randomSecret(), 12*3600)
	http.Redirect(w, r, "/admin/settings", 303)
}
