package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestV2BoardMigration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	app, err := newApp(s, []string{"migration-token"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.bootstrapAdmin(ctx, "owner@example.com", "migration-password", false); err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO boards(id,name,slug,description,created_at) VALUES(7,'Existing board','existing-board','Existing description','2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	id := int64(7)
	opener, err := s.createInBoard(ctx, 0, &id, "Historical project discussion", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.create(ctx, opener, ">>1 Preserved reply", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.create(ctx, 0, "Free discussion", nil); err != nil {
		t.Fatal(err)
	}
	if err = s.saveSettings(ctx, "https://example.com", 2); err != nil {
		t.Fatal(err)
	}
	oldPrompt := "Custom rules stay. Projects: GET /api/projects; POST /api/projects/{project_id}/threads. Read project_id, project_name, ProjectID, and ProjectName. A PROJECT has projects. Keep projection unchanged.\n"
	expectedPrompt := "Custom rules stay. Boards: GET /api/boards; POST /api/boards/{board_id}/threads. Read board_id, board_name, BoardID, and BoardName. A BOARD has boards. Keep projection unchanged.\n"
	if _, err = s.db.Exec(`UPDATE settings SET onboarding_prompt=?`, oldPrompt); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO sessions(hash,expires_at) VALUES(?,?)`, secretHash("saved-session"), time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the exact v2 table/column/index names to exercise a real upgrade.
	_, err = s.db.Exec(`BEGIN;
 ALTER TABLE boards RENAME TO projects;
 ALTER TABLE threads RENAME COLUMN board_id TO project_id;
 DROP INDEX threads_board;
 CREATE INDEX threads_project ON threads(project_id,bumped_at DESC,last_post_id DESC);
 PRAGMA user_version=2;
 COMMIT;`)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	s, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	app, err = newApp(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != 3 {
		t.Fatalf("schema version %d", version)
	}
	board, err := s.board(ctx, 7)
	if err != nil || board.Name != "Existing board" || board.ThreadCount != 1 || board.Permalink != "/boards/7/threads" {
		t.Fatalf("board migration: %+v %v", board, err)
	}
	thread, err := s.thread(ctx, opener)
	if err != nil || thread.BoardID == nil || *thread.BoardID != 7 || !thread.Full || len(thread.Posts) != 2 || thread.Posts[0].Text != "Historical project discussion" || len(thread.Posts[0].Backlinks) != 1 {
		t.Fatalf("thread migration: %+v %v", thread, err)
	}
	free, _, err := s.list(ctx, 1)
	if err != nil || len(free) != 1 || free[0].BoardID != nil {
		t.Fatalf("free threads: %+v %v", free, err)
	}
	settings, err := s.settings(ctx)
	if err != nil || settings.OnboardingPrompt != expectedPrompt || settings.PublicURL != "https://example.com" || settings.PostLimit != 2 {
		t.Fatalf("settings migration: %+v %v", settings, err)
	}
	valid, err := app.validToken(ctx, "migration-token")
	if err != nil || !valid {
		t.Fatal("lost posting credential")
	}
	token, err := app.decryptToken(ctx, 1)
	if err != nil || token != "migration-token" {
		t.Fatal("lost encrypted token")
	}
	var hash string
	if err = s.db.QueryRow(`SELECT password_hash FROM admin WHERE id=1`).Scan(&hash); err != nil || !checkPassword(hash, "migration-password") {
		t.Fatal("lost admin credentials")
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE hash=?`, secretHash("saved-session")).Scan(&count); err != nil || count != 1 {
		t.Fatal("lost session")
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('projects','threads_project')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("legacy tables remain")
	}
	rows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	hasViolation := rows.Next()
	rows.Close()
	if hasViolation {
		t.Fatal("broken foreign key after migration")
	}
	// Both membership and AUTOINCREMENT must continue working after the rename.
	f := fixture{s, app.handler()}
	w := f.request("POST", "/api/boards", "application/json", "migration-token", strings.NewReader(`{"name":"New board","slug":"new-board"}`))
	expectCode(t, w, 201)
	var created struct{ Board Board }
	if err = json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Board.ID <= 7 {
		t.Fatal("board IDs were reused")
	}
	expectCode(t, f.request("POST", "/api/boards/7/threads", "application/json", "migration-token", strings.NewReader(`{"text":"New discussion"}`)), 201)
}

func TestBoardAPIContract(t *testing.T) {
	f := setup(t)
	w := f.request("POST", "/api/boards", "application/json", "test-token", strings.NewReader(`{"name":"My board","slug":"my-board"}`))
	expectCode(t, w, 201)
	var result map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["board"] == nil || result["project"] != nil {
		t.Fatal("wrong create response keys")
	}
	expectCode(t, f.request("POST", "/api/boards/1/threads", "application/json", "test-token", strings.NewReader(`{"text":"Board discussion"}`)), 201)
	for _, path := range []string{"/api/boards", "/api/boards/1/threads", "/api/threads/1", "/onboarding"} {
		w = f.request("GET", path, "", "", nil)
		expectCode(t, w, 200)
		for _, legacy := range []string{`"project":`, `"projects":`, `"project_id":`, `"project_name":`, "/projects/", "/api/projects"} {
			if strings.Contains(strings.ToLower(w.Body.String()), legacy) {
				t.Fatalf("legacy API term %q in %s", legacy, path)
			}
		}
	}
	w = f.request("GET", "/api/boards/1/threads", "", "", nil)
	var index struct {
		Board   Board
		Threads []map[string]json.RawMessage
	}
	if err := json.Unmarshal(w.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if index.Board.ID != 1 || index.Threads[0]["board_id"] == nil || index.Threads[0]["board_name"] == nil {
		t.Fatal("board fields missing")
	}
	for _, path := range []string{"/", "/boards/1/threads", "/threads/1"} {
		w = f.request("GET", path, "", "", nil)
		expectCode(t, w, 200)
		if strings.Contains(strings.ToLower(w.Body.String()), "project") {
			t.Fatalf("legacy term in HTML %s", path)
		}
	}
	for _, path := range []string{"/projects/1/threads", "/api/projects", "/api/projects/1/threads"} {
		expectCode(t, f.request("GET", path, "", "", nil), 404)
	}
	expectCode(t, f.request("POST", "/api/projects", "application/json", "test-token", strings.NewReader(`{"name":"Old endpoint"}`)), 404)
}
