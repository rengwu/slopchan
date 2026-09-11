package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func portal(t *testing.T) (*App, *adminBrowser) {
	t.Helper()
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	if err = s.bootstrapAdmin(context.Background(), "owner@example.com", "long-test-password", false); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(s, []string{"legacy-token"})
	if err != nil {
		t.Fatal(err)
	}
	return a, &adminBrowser{handler: a.handler(), cookies: map[string]*http.Cookie{}}
}

type adminBrowser struct {
	handler  http.Handler
	cookies  map[string]*http.Cookie
	insecure bool
}

func (b *adminBrowser) req(method, path string, values url.Values) *httptest.ResponseRecorder {
	if values == nil {
		values = url.Values{}
	}
	scheme, csrfName := "https", csrfCookie
	if b.insecure {
		scheme, csrfName = "http", "slopchan_csrf"
	}
	if c := b.cookies[csrfName]; c != nil && !values.Has("csrf") {
		values.Set("csrf", c.Value)
	}
	r := httptest.NewRequest(method, scheme+"://board.example"+path, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range b.cookies {
		if !b.insecure || !c.Secure {
			r.AddCookie(c)
		}
	}
	w := httptest.NewRecorder()
	b.handler.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return w
}
func (b *adminBrowser) login(t *testing.T) {
	t.Helper()
	w := b.req("GET", "/admin", nil)
	if w.Code != 200 {
		t.Fatalf("login page: %d %s", w.Code, w.Body)
	}
	w = b.req("POST", "/admin/login", url.Values{"email": {"owner@example.com"}, "password": {"long-test-password"}})
	if w.Code != 303 || w.Header().Get("Location") != "/admin/settings" {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
}
func expectCode(t *testing.T, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Fatalf("wanted %d, got %d: %s", code, w.Code, w.Body)
	}
}

func TestAdminSecurityAndCredentialChanges(t *testing.T) {
	a, b := portal(t)
	r := httptest.NewRequest("GET", "http://board.example/admin", nil)
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	expectCode(t, w, 426)
	r.Header.Set("X-Forwarded-Proto", "https")
	w = httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	expectCode(t, w, 426)
	a.trustProxy = true
	w = httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	expectCode(t, w, 200)
	a.trustProxy = false
	expectCode(t, b.req("GET", "/admin/tokens", nil), 303)
	b.req("GET", "/admin", nil)
	expectCode(t, b.req("POST", "/admin/login", url.Values{"email": {"owner@example.com"}, "password": {"wrong"}}), 401)
	expectCode(t, b.req("POST", "/admin/login", url.Values{"csrf": {"bad"}, "email": {"owner@example.com"}, "password": {"long-test-password"}}), 403)
	r = httptest.NewRequest("POST", "https://board.example/admin/login", strings.NewReader(""))
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	expectCode(t, w, 403)
	b.login(t)
	for _, name := range []string{sessionCookie, csrfCookie} {
		c := b.cookies[name]
		if c == nil || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/admin" {
			t.Fatalf("unsafe cookie: %+v", c)
		}
	}
	for _, path := range []string{"/admin/settings", "/admin/tokens", "/admin/account", "/admin/onboarding"} {
		expectCode(t, b.req("GET", path, nil), 200)
	}
	// A bearer token is not an admin session.
	f := fixture{a.store, a.handler()}
	expectCode(t, f.request("GET", "https://board.example/admin/tokens", "", "legacy-token", nil), 303)
	oldSession := *b.cookies[sessionCookie]
	expectCode(t, b.req("POST", "/admin/account", url.Values{"email": {"new@example.com"}, "current_password": {"wrong"}, "password": {"new-long-password"}, "password_confirm": {"new-long-password"}}), 400)
	expectCode(t, b.req("POST", "/admin/account", url.Values{"email": {"new@example.com"}, "current_password": {"long-test-password"}, "password": {"new-long-password"}, "password_confirm": {"new-long-password"}}), 303)
	if _, ok := b.cookies[sessionCookie]; ok {
		t.Fatal("credential change kept session cookie")
	}
	b.cookies[sessionCookie] = &oldSession
	expectCode(t, b.req("GET", "/admin/settings", nil), 303)
	delete(b.cookies, sessionCookie)
	// Old environment settings must not silently undo a portal edit.
	if err := a.store.bootstrapAdmin(context.Background(), "owner@example.com", "long-test-password", false); err != nil {
		t.Fatal(err)
	}
	expectCode(t, b.req("POST", "/admin/login", url.Values{"email": {"owner@example.com"}, "password": {"long-test-password"}}), 401)
	expectCode(t, b.req("POST", "/admin/login", url.Values{"email": {"new@example.com"}, "password": {"new-long-password"}}), 303)
	var hash string
	if err := a.store.db.QueryRow(`SELECT password_hash FROM admin`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "new-long-password") || !checkPassword(hash, "new-long-password") {
		t.Fatal("password not hashed")
	}
	expectCode(t, b.req("POST", "/admin/logout", nil), 303)
	expectCode(t, b.req("GET", "/admin/settings", nil), 303)
	if err := a.store.bootstrapAdmin(context.Background(), "reset@example.com", "reset-long-password", true); err != nil {
		t.Fatal(err)
	}
	expectCode(t, b.req("POST", "/admin/login", url.Values{"email": {"reset@example.com"}, "password": {"reset-long-password"}}), 303)
	if _, err := a.store.db.Exec(`UPDATE sessions SET expires_at=0`); err != nil {
		t.Fatal(err)
	}
	expectCode(t, b.req("GET", "/admin/settings", nil), 303)
}

func TestAdminInsecureHTTP(t *testing.T) {
	a, b := portal(t)
	b.insecure = true
	expectCode(t, b.req("GET", "/admin", nil), http.StatusUpgradeRequired)
	a.allowInsecureAdmin = true
	expectCode(t, b.req("GET", "/admin/settings", nil), 303)
	b.login(t)
	for _, name := range []string{"slopchan_session", "slopchan_csrf"} {
		c := b.cookies[name]
		if c == nil || c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/admin" {
			t.Fatalf("invalid HTTP cookie: %+v", c)
		}
	}
	if b.cookies[sessionCookie] != nil || b.cookies[csrfCookie] != nil {
		t.Fatal("HTTP response used secure cookie names")
	}
	for _, path := range []string{"/admin/settings", "/admin/tokens", "/admin/account", "/admin/onboarding"} {
		expectCode(t, b.req("GET", path, nil), 200)
	}
	expectCode(t, b.req("POST", "/admin/settings", url.Values{"csrf": {"bad"}}), 403)
	r := httptest.NewRequest("POST", "http://board.example/admin/settings", nil)
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	expectCode(t, w, 403)
	expectCode(t, b.req("POST", "/admin/settings", url.Values{"public_url": {"http://board.example"}, "post_limit": {"50"}}), 303)
	// HTTPS ignores unprefixed cookies, even when HTTP access is enabled.
	b.insecure = false
	expectCode(t, b.req("GET", "/admin/settings", nil), 303)
	b.insecure = true
	a.allowInsecureAdmin = false
	expectCode(t, b.req("GET", "/admin/settings", nil), http.StatusUpgradeRequired)
	a.allowInsecureAdmin = true
	oldSession := *b.cookies["slopchan_session"]
	w = b.req("POST", "/admin/logout", nil)
	expectCode(t, w, 303)
	for _, c := range w.Result().Cookies() {
		if c.Name == "slopchan_session" && (c.MaxAge >= 0 || c.Secure) {
			t.Fatalf("invalid HTTP logout cookie: %+v", c)
		}
	}
	if b.cookies["slopchan_session"] != nil {
		t.Fatal("logout kept HTTP session cookie")
	}
	b.cookies["slopchan_session"] = &oldSession
	expectCode(t, b.req("GET", "/admin/settings", nil), 303)
	delete(b.cookies, "slopchan_session")
	b.login(t)
	expectCode(t, b.req("POST", "/admin/account", url.Values{"email": {"new@example.com"}, "current_password": {"long-test-password"}, "password": {"new-long-password"}, "password_confirm": {"new-long-password"}}), 303)
	if b.cookies["slopchan_session"] != nil {
		t.Fatal("credential change kept HTTP session cookie")
	}
}

func TestAdminInsecureOptOutPreservesHTTPSCookies(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		t.Run(fmt.Sprintf("proxy=%t", proxy), func(t *testing.T) {
			a, b := portal(t)
			a.allowInsecureAdmin = true
			if proxy {
				a.trustProxy = true
				b.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					r.TLS = nil
					r.Header.Set("X-Forwarded-Proto", "https")
					a.handler().ServeHTTP(w, r)
				})
			}
			b.login(t)
			for _, name := range []string{sessionCookie, csrfCookie} {
				c := b.cookies[name]
				if c == nil || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/admin" {
					t.Fatalf("unsafe HTTPS cookie: %+v", c)
				}
			}
			expectCode(t, b.req("GET", "/admin/settings", nil), 200)
		})
	}
}

func TestTokenLifecycleAndOnboarding(t *testing.T) {
	a, b := portal(t)
	b.login(t)
	expectCode(t, b.req("POST", "/admin/tokens", url.Values{"action": {"create"}, "name": {"Laptop <agents>"}}), 303)
	var id int64
	if err := a.store.db.QueryRow(`SELECT MAX(id) FROM access_tokens`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	expectCode(t, b.req("POST", "/admin/tokens", url.Values{"action": {"download"}, "id": {fmt.Sprint(id)}}), 400)
	expectCode(t, b.req("POST", "/admin/settings", url.Values{"public_url": {"https://board.example/"}, "post_limit": {"3"}}), 303)
	w := b.req("POST", "/admin/tokens", url.Values{"action": {"download"}, "id": {fmt.Sprint(id)}})
	expectCode(t, w, 200)
	if !strings.Contains(w.Header().Get("Content-Disposition"), ".env.slopchan") || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "SLOPCHAN_URL='https://board.example'") {
		t.Fatal("bad env download")
	}
	token := regexp.MustCompile(`SLOPCHAN_TOKEN='([a-f0-9]+)'`).FindStringSubmatch(w.Body.String())[1]
	var encrypted []byte
	if err := a.store.db.QueryRow(`SELECT secret FROM access_tokens WHERE id=?`, id).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(token)) {
		t.Fatal("plaintext stored token")
	}
	f := fixture{a.store, a.handler()}
	expectCode(t, f.request("POST", "/api/threads", "application/json", token, strings.NewReader(`{"text":"token works"}`)), 201)
	listing := b.req("GET", "/admin/tokens", nil)
	expectCode(t, listing, 200)
	if strings.Contains(listing.Body.String(), token) || !strings.Contains(listing.Body.String(), "Laptop &lt;agents&gt;") {
		t.Fatal("token leaked or name unsafe")
	}
	var used string
	a.store.db.QueryRow(`SELECT last_used_at FROM access_tokens WHERE id=?`, id).Scan(&used)
	if used == "" {
		t.Fatal("usage not tracked")
	}
	expectCode(t, b.req("POST", "/admin/tokens", url.Values{"action": {"revoke"}, "id": {fmt.Sprint(id)}}), 303)
	expectCode(t, f.request("POST", "/api/threads", "application/json", token, strings.NewReader(`{"text":"blocked"}`)), 401)
	expectCode(t, b.req("POST", "/admin/tokens", url.Values{"action": {"download"}, "id": {fmt.Sprint(id)}}), 400)
	expectCode(t, b.req("POST", "/admin/tokens", url.Values{"action": {"revoke"}, "id": {"1"}}), 303)
	// Reimporting startup tokens cannot reactivate a revoked token.
	reopened, err := newApp(a.store, []string{"legacy-token"})
	if err != nil {
		t.Fatal(err)
	}
	valid, err := reopened.validToken(context.Background(), "legacy-token")
	if err != nil || valid {
		t.Fatalf("revoked launch token accepted: %v", err)
	}
	var overview struct {
		Instructions string `json:"instructions"`
		Limit        int    `json:"thread_max_post_count"`
		PublicURL    string `json:"public_url"`
	}
	w = f.request("GET", "/onboarding", "", "", nil)
	expectCode(t, w, 200)
	if err = json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
		t.Fatal(err)
	}
	if overview.Instructions != defaultOnboarding || overview.Limit != 3 || overview.PublicURL != "https://board.example" || strings.Contains(w.Body.String(), token) {
		t.Fatal("bad public onboarding")
	}
	expectCode(t, b.req("POST", "/admin/onboarding", url.Values{"prompt": {"Custom <instructions>"}, "action": {"save"}}), 303)
	w = f.request("GET", "/onboarding", "", "", nil)
	if !strings.Contains(w.Body.String(), "Custom ") {
		t.Fatal("custom prompt absent")
	}
	expectCode(t, b.req("POST", "/admin/onboarding", url.Values{"action": {"reset"}}), 303)
	settings, err := a.store.settings(context.Background())
	if err != nil || settings.OnboardingPrompt != defaultOnboarding {
		t.Fatal("reset failed")
	}
	for _, raw := range []string{"javascript:alert(1)", "https://user:pass@host", "https://host/path", "https://host?a=b", "https://host/#x", "https://host\nSLOPCHAN_TOKEN=x"} {
		if _, err := validatePublicURL(raw); err == nil {
			t.Fatalf("accepted invalid origin %q", raw)
		}
	}
}

func TestBoardsAndDynamicLimits(t *testing.T) {
	f := setup(t)
	create := func(body string) *httptest.ResponseRecorder {
		return f.request("POST", "/api/boards", "application/json", "test-token", strings.NewReader(body))
	}
	expectCode(t, f.request("POST", "/api/boards", "application/json", "", strings.NewReader(`{"name":"One"}`)), 401)
	w := create(`{"name":"My Board","slug":"owner-repo","description":"A useful board"}`)
	expectCode(t, w, 201)
	var result struct{ Board Board }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	p := result.Board
	w = create(`{"name":"Different name","slug":"owner-repo"}`)
	expectCode(t, w, 200)
	expectCode(t, create(`{"name":"Broken","slug":"../bad"}`), 400)
	expectCode(t, create(`{"name":""}`), 400)
	f.add(t, 0, "Free discussion")
	w = f.request("POST", p.APIURL, "application/json", "test-token", strings.NewReader(`{"text":"Board discussion"}`))
	expectCode(t, w, 201)
	var write struct {
		Post   Post
		Thread Thread
	}
	json.Unmarshal(w.Body.Bytes(), &write)
	if write.Thread.BoardID == nil || *write.Thread.BoardID != p.ID {
		t.Fatal("board thread unassigned")
	}
	boardThread := write.Thread.ID
	w = f.request("GET", "/api/threads", "", "", nil)
	if strings.Contains(w.Body.String(), "Board discussion") {
		t.Fatal("board leaked into free threads")
	}
	w = f.request("GET", p.APIURL, "", "", nil)
	var board struct{ Board Board }
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil || board.Board.ThreadCount != 1 {
		t.Fatalf("inconsistent board count: %+v %v", board, err)
	}
	if strings.Contains(w.Body.String(), "Free discussion") || !strings.Contains(w.Body.String(), "Board discussion") {
		t.Fatal("wrong board listing")
	}
	expectCode(t, f.request("POST", "/api/boards/999/threads", "application/json", "test-token", strings.NewReader(`{"text":"missing"}`)), 404)
	expectCode(t, f.request("GET", "/api/boards/nope/threads", "", "", nil), 404)
	w = f.request("GET", "/onboarding", "", "", nil)
	if !strings.Contains(w.Body.String(), "owner-repo") || !strings.Contains(w.Body.String(), "Board discussion") {
		t.Fatal("missing onboarding board brief")
	}
	if err := f.store.saveSettings(context.Background(), "https://example.com", 2); err != nil {
		t.Fatal(err)
	}
	f.add(t, boardThread, "Last reply")
	expectCode(t, f.request("POST", fmt.Sprintf("/api/threads/%d/posts", boardThread), "application/json", "test-token", strings.NewReader(`{"text":"too late"}`)), 409)
	// Full threads stay closed when the limit is raised.
	if err := f.store.saveSettings(context.Background(), "https://example.com", 300); err != nil {
		t.Fatal(err)
	}
	thread, err := f.store.thread(context.Background(), boardThread)
	if err != nil || !thread.Full || thread.PostCount != 2 || thread.PostLimit != 300 {
		t.Fatalf("bad full thread: %+v %v", thread, err)
	}
	fresh := f.add(t, 0, "Larger capacity")
	// Exercise beyond the old SQLite CHECK constraint.
	for i := 1; i < 202; i++ {
		if _, err = f.store.create(context.Background(), fresh.ID, "reply", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.store.saveSettings(context.Background(), "https://example.com", 100); err != nil {
		t.Fatal(err)
	}
	thread, err = f.store.thread(context.Background(), fresh.ID)
	if err != nil || !thread.Full || len(thread.Posts) != 202 {
		t.Fatal("limit reduction removed history")
	}
	if err = f.store.saveSettings(context.Background(), "https://example.com", 1); err != nil {
		t.Fatal(err)
	}
	opener := f.add(t, 0, "Immediately full")
	thread, err = f.store.thread(context.Background(), opener.ID)
	if err != nil || !thread.Full {
		t.Fatal("limit-one thread open")
	}
	// The public HTML shows the board name and board navigation.
	w = f.request("GET", fmt.Sprintf("/threads/%d", boardThread), "", "", nil)
	expectCode(t, w, 200)
	if !strings.Contains(w.Body.String(), "My Board") {
		t.Fatal("missing board breadcrumb")
	}
}

func TestV1MigrationPreservesData(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "slopchan.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE posts(id INTEGER PRIMARY KEY,thread_id INTEGER,text TEXT,created_at TEXT,removed INTEGER DEFAULT 0,image_name TEXT DEFAULT '',image_mime TEXT DEFAULT '',image_bytes INTEGER DEFAULT 0,image_width INTEGER DEFAULT 0,image_height INTEGER DEFAULT 0);
 CREATE TABLE threads(id INTEGER PRIMARY KEY REFERENCES posts(id),post_count INTEGER CHECK(post_count BETWEEN 1 AND 200),last_post_id INTEGER REFERENCES posts(id),bumped_at TEXT);
 CREATE INDEX threads_bump ON threads(bumped_at DESC,last_post_id DESC);
 INSERT INTO posts(id,thread_id,text,created_at) VALUES(1,1,'Existing content','2026-01-01T00:00:00Z');
 INSERT INTO threads VALUES(1,1,1,'2026-01-01T00:00:00Z');PRAGMA user_version=1;`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.threadMeta(context.Background(), 1)
	if err != nil || thread.BoardID != nil || thread.PostCount != 1 {
		t.Fatalf("migration: %+v %v", thread, err)
	}
	settings, err := s.settings(context.Background())
	if err != nil || settings.PostLimit != 200 {
		t.Fatalf("migration changed existing limit: %+v %v", settings, err)
	}
	var text string
	if err = s.db.QueryRow(`SELECT text FROM posts WHERE id=1`).Scan(&text); err != nil || text != "Existing content" {
		t.Fatal("lost old post")
	}
	s.db.Close()
	s, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Close()
}

func TestConcurrentBoardCreationAndTokenPersistence(t *testing.T) {
	a, _ := portal(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := fixture{a.store, a.handler()}
			w := f.request("POST", "/api/boards", "application/json", "legacy-token", strings.NewReader(`{"name":"Shared","slug":"shared"}`))
			if w.Code != 200 && w.Code != 201 {
				t.Errorf("board race: %d %s", w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	ps, err := a.store.boards(context.Background())
	if err != nil || len(ps) != 1 {
		t.Fatal("duplicate boards")
	}
	if err = a.saveToken(context.Background(), "persist", "saved-secret"); err != nil {
		t.Fatal(err)
	}
	var id int64
	a.store.db.QueryRow(`SELECT id FROM access_tokens WHERE name='persist'`).Scan(&id)
	s2, err := openStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.db.Close()
	a2, err := newApp(s2, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := a2.decryptToken(context.Background(), id)
	if err != nil || secret != "saved-secret" {
		t.Fatalf("restart: %s %v", secret, err)
	}
	keyInfo, err := os.Stat(filepath.Join(a.store.dir, "token.key"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows uses inherited ACLs, not Unix permission bits (os.Chmod only
	// controls the read-only attribute there). The installer protects its root.
	if runtime.GOOS != "windows" && keyInfo.Mode().Perm() != 0600 {
		t.Fatal("key permissions")
	}
}

func TestLoginRateLimitAndExpiredSession(t *testing.T) {
	a, b := portal(t)
	b.req("GET", "/admin", nil)
	for i := 0; i < 10; i++ {
		expectCode(t, b.req("POST", "/admin/login", url.Values{"email": {"wrong@example.com"}, "password": {"wrong"}}), 401)
	}
	w := b.req("POST", "/admin/login", url.Values{"email": {"owner@example.com"}, "password": {"long-test-password"}})
	expectCode(t, w, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("no retry delay")
	}
	a.loginMu.Lock()
	a.loginAttempts = []time.Time{time.Now().Add(-2 * time.Minute)}
	a.loginMu.Unlock()
	b.login(t)
}

func TestPublicOnboardingDoesNotRequireBearer(t *testing.T) {
	f := setup(t)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	response, err := http.Get(server.URL + "/onboarding")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || !strings.Contains(string(body), "instructions") {
		t.Fatalf("onboarding unavailable: %s", body)
	}
}

func TestBoardPagination(t *testing.T) {
	f := setup(t)
	w := f.request("POST", "/api/boards", "application/json", "test-token", strings.NewReader(`{"name":"Paginated"}`))
	expectCode(t, w, 201)
	var result struct{ Board Board }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	id := result.Board.ID
	for i := 0; i < 22; i++ {
		body := fmt.Sprintf("board post %d", i)
		if i == 21 {
			body = strings.Repeat("界", 300)
		}
		if _, err := f.store.createInBoard(context.Background(), 0, &id, body, nil); err != nil {
			t.Fatal(err)
		}
	}
	w = f.request("GET", result.Board.APIURL, "", "", nil)
	var page struct {
		Threads []Thread
		Next    string
		Board   Board
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Threads) != 20 || page.Board.ThreadCount != 22 || page.Next != result.Board.APIURL+"?page=2" {
		t.Fatalf("bad first page: %+v", page)
	}
	w = f.request("GET", page.Next, "", "", nil)
	expectCode(t, w, 200)
	var last struct {
		Threads []Thread
		Next    *string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &last); err != nil {
		t.Fatal(err)
	}
	if len(last.Threads) != 2 || last.Next != nil {
		t.Fatal("bad final page")
	}
	w = f.request("GET", "/onboarding", "", "", nil)
	var briefing struct{ Boards []onboardingBoard }
	if err := json.Unmarshal(w.Body.Bytes(), &briefing); err != nil {
		t.Fatal(err)
	}
	if len(briefing.Boards[0].LatestThreads) != 3 || briefing.Boards[0].ThreadCount != 22 {
		t.Fatal("bad onboarding brief")
	}
	brief := briefing.Boards[0].LatestThreads[0]
	if brief.Preview != strings.Repeat("界", 239)+"…" {
		t.Fatalf("bad brief excerpt: %q", brief.Preview)
	}
	var raw struct {
		Boards []map[string]json.RawMessage
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	var threads []map[string]json.RawMessage
	if err := json.Unmarshal(raw.Boards[0]["latest_threads"], &threads); err != nil {
		t.Fatal(err)
	}
	if len(threads[0]) != 5 || threads[0]["posts"] != nil || raw.Boards[0]["created_at"] != nil || raw.Boards[0]["permalink"] != nil {
		t.Fatal("onboarding contains unnecessary metadata")
	}
	w = f.request("GET", brief.APIURL, "", "", nil)
	expectCode(t, w, 200)
	if !strings.Contains(w.Body.String(), strings.Repeat("界", 300)) {
		t.Fatal("full thread must retain text omitted from onboarding")
	}
}
