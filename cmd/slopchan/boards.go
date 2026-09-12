package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Board struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	Description   string   `json:"description"`
	CreatedAt     string   `json:"created_at"`
	ThreadCount   int      `json:"thread_count"`
	Permalink     string   `json:"permalink"`
	APIURL        string   `json:"api_url"`
	LatestThreads []Thread `json:"latest_threads,omitempty"`
}

func (p *Board) links() {
	p.Permalink = fmt.Sprintf("/boards/%d/threads", p.ID)
	p.APIURL = fmt.Sprintf("/api/boards/%d/threads", p.ID)
}
func (s *Store) boards(ctx context.Context) ([]Board, error) {
	s, done, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	rows, err := s.readTx.QueryContext(ctx, `SELECT p.id,p.name,p.slug,p.description,p.created_at,COUNT(t.id) FROM boards p LEFT JOIN threads t ON t.board_id=p.id GROUP BY p.id ORDER BY p.name COLLATE NOCASE,p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Board{}
	for rows.Next() {
		var p Board
		if err = rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.CreatedAt, &p.ThreadCount); err != nil {
			return nil, err
		}
		p.links()
		result = append(result, p)
	}
	return result, rows.Err()
}
func (s *Store) board(ctx context.Context, id int64) (Board, error) {
	var p Board
	err := s.queryRow(ctx, `SELECT p.id,p.name,p.slug,p.description,p.created_at,(SELECT COUNT(*) FROM threads t WHERE t.board_id=p.id) FROM boards p WHERE p.id=?`, id).Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.CreatedAt, &p.ThreadCount)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	p.links()
	return p, err
}

var slugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var slugSeparators = regexp.MustCompile(`[^a-z0-9]+`)

func (a *App) createBoard(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	raw, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(raw) {
		a.problem(w, r, 400, "invalid_board", "Provide a UTF-8 JSON object up to 16 KiB.")
		return
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil || dec.Decode(new(any)) != io.EOF {
		a.problem(w, r, 400, "invalid_board", "Provide a JSON object with name, optional slug, and optional description.")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = strings.ToLower(strings.TrimSpace(input.Slug))
	input.Description = strings.TrimSpace(input.Description)
	if input.Slug == "" {
		input.Slug = strings.Trim(slugSeparators.ReplaceAllString(strings.ToLower(input.Name), "-"), "-")
	}
	if !utf8.ValidString(input.Name+input.Description) || input.Name == "" || utf8.RuneCountInString(input.Name) > 100 || utf8.RuneCountInString(input.Description) > 1000 || len(input.Slug) > 80 || !slugRE.MatchString(input.Slug) {
		a.problem(w, r, 400, "invalid_board", "Use a name of 1–100 characters, a lowercase slug of 1–80 letters/digits separated by hyphens, and a description up to 1,000 characters.")
		return
	}
	result, err := a.store.db.ExecContext(r.Context(), `INSERT INTO boards(name,slug,description,created_at) VALUES(?,?,?,?) ON CONFLICT(slug) DO NOTHING`, input.Name, input.Slug, input.Description, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		a.internal(w, r, err)
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		a.internal(w, r, err)
		return
	}
	var id int64
	if err = a.store.db.QueryRowContext(r.Context(), `SELECT id FROM boards WHERE slug=?`, input.Slug).Scan(&id); err != nil {
		a.internal(w, r, err)
		return
	}
	p, err := a.store.board(r.Context(), id)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	status := 201
	if n == 0 {
		status = 200
	}
	w.Header().Set("Location", p.Permalink)
	sendJSON(w, status, map[string]any{"board": p, "created": n == 1})
}
func (a *App) getBoards(w http.ResponseWriter, r *http.Request) {
	boards, err := a.store.boards(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	sendJSON(w, 200, map[string]any{"boards": boards})
}
