package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const postLimit = 200
const pageSize = 20
const textLimit = 10000
const previewLimit = 2000

var errNotFound = errors.New("not found")
var errFull = errors.New("thread is full")
var referenceRE = regexp.MustCompile(`>>([1-9][0-9]*)`)

type Attachment struct {
	URL    string `json:"url"`
	MIME   string `json:"mime"`
	Bytes  int64  `json:"bytes"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type Post struct {
	ID         int64       `json:"id"`
	ThreadID   int64       `json:"thread_id"`
	Text       string      `json:"text"`
	CreatedAt  string      `json:"created_at"`
	Removed    bool        `json:"removed"`
	Image      *Attachment `json:"image"`
	References []int64     `json:"references"`
	Backlinks  []int64     `json:"backlinks"`
	Permalink  string      `json:"permalink"`
	APIURL     string      `json:"api_url"`
	Truncated  bool        `json:"truncated"`
}

type Thread struct {
	ID         int64  `json:"id"`
	PostCount  int    `json:"post_count"`
	PostLimit  int    `json:"post_limit"`
	LastPostID int64  `json:"last_post_id"`
	BumpedAt   string `json:"bumped_at"`
	Full       bool   `json:"full"`
	Permalink  string `json:"permalink"`
	APIURL     string `json:"api_url"`
	Posts      []Post `json:"posts"`
}

type Store struct {
	db     *sql.DB
	dir    string
	readTx *sql.Tx
}

// A copied store shares a read transaction across all queries used to assemble
// one response. Nested readers reuse it; the live Store itself is never mutated.
func (s *Store) snapshot(ctx context.Context) (*Store, func(), error) {
	if s.readTx != nil {
		return s, func() {}, nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	copy := *s
	copy.readTx = tx
	return &copy, func() { tx.Rollback() }, nil
}

func (s *Store) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	if s.readTx != nil {
		return s.readTx.QueryRowContext(ctx, query, args...)
	}
	return s.db.QueryRowContext(ctx, query, args...)
}

func openStore(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(abs, "images"), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(abs, "slopchan.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	if _, err = db.Exec(`PRAGMA busy_timeout=10000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON;`); err != nil {
		return fail(err)
	}
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fail(err)
	}
	if version > 1 {
		return fail(fmt.Errorf("database schema %d is newer than this application", version))
	}
	if version == 0 {
		_, err = db.Exec(`BEGIN IMMEDIATE;
   CREATE TABLE posts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id INTEGER REFERENCES posts(id),
    text TEXT NOT NULL,
    created_at TEXT NOT NULL,
    removed INTEGER NOT NULL DEFAULT 0,
    image_name TEXT NOT NULL DEFAULT '',
    image_mime TEXT NOT NULL DEFAULT '',
    image_bytes INTEGER NOT NULL DEFAULT 0,
    image_width INTEGER NOT NULL DEFAULT 0,
    image_height INTEGER NOT NULL DEFAULT 0
   );
   CREATE INDEX posts_thread ON posts(thread_id,id);
   CREATE TABLE threads (
    id INTEGER PRIMARY KEY REFERENCES posts(id),
    post_count INTEGER NOT NULL CHECK(post_count BETWEEN 1 AND 200),
    last_post_id INTEGER NOT NULL REFERENCES posts(id),
    bumped_at TEXT NOT NULL
   );
   CREATE INDEX threads_bump ON threads(bumped_at DESC,last_post_id DESC);
   CREATE TABLE refs (
    source INTEGER NOT NULL REFERENCES posts(id),
    target INTEGER NOT NULL REFERENCES posts(id),
    PRIMARY KEY(source,target)
   );
   CREATE INDEX refs_target ON refs(target,source);
   CREATE VIRTUAL TABLE posts_fts USING fts5(text, content='posts', content_rowid='id', tokenize='unicode61');
   CREATE TRIGGER posts_ai AFTER INSERT ON posts BEGIN
    INSERT INTO posts_fts(rowid,text) VALUES(new.id,new.text);
   END;
   CREATE TRIGGER posts_au AFTER UPDATE OF text ON posts BEGIN
    INSERT INTO posts_fts(posts_fts,rowid,text) VALUES('delete',old.id,old.text);
    INSERT INTO posts_fts(rowid,text) VALUES(new.id,new.text);
   END;
   PRAGMA user_version=1;
   COMMIT;`)
		if err != nil {
			return fail(err)
		}
	}
	return &Store{db: db, dir: abs}, nil
}

// A dedicated connection and BEGIN IMMEDIATE serialize the count check and insert
// across both goroutines and other processes (including owner commands).
func (s *Store) create(ctx context.Context, threadID int64, body string, img *storedImage) (int64, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return 0, err
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	if threadID != 0 {
		var count int
		err = conn.QueryRowContext(ctx, `SELECT post_count FROM threads WHERE id=?`, threadID).Scan(&count)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errNotFound
		}
		if err != nil {
			return 0, err
		}
		if count >= postLimit {
			return 0, errFull
		}
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	if img == nil {
		img = &storedImage{}
	}
	var parent any
	if threadID != 0 {
		parent = threadID
	}
	result, err := conn.ExecContext(ctx, `INSERT INTO posts(thread_id,text,created_at,image_name,image_mime,image_bytes,image_width,image_height) VALUES(?,?,?,?,?,?,?,?)`, parent, body, now, img.Name, img.MIME, img.Bytes, img.Width, img.Height)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if threadID == 0 {
		threadID = id
		if _, err = conn.ExecContext(ctx, `UPDATE posts SET thread_id=? WHERE id=?`, id, id); err != nil {
			return 0, err
		}
		_, err = conn.ExecContext(ctx, `INSERT INTO threads(id,post_count,last_post_id,bumped_at) VALUES(?,1,?,?)`, id, id, now)
	} else {
		_, err = conn.ExecContext(ctx, `UPDATE threads SET post_count=post_count+1,last_post_id=?,bumped_at=? WHERE id=?`, id, now, threadID)
	}
	if err != nil {
		return 0, err
	}
	for _, match := range referenceRE.FindAllStringSubmatch(body, -1) {
		target, e := strconv.ParseInt(match[1], 10, 64)
		if e != nil || target >= id {
			continue
		}
		if _, err = conn.ExecContext(ctx, `INSERT OR IGNORE INTO refs(source,target) SELECT ?,id FROM posts WHERE id=?`, id, target); err != nil {
			return 0, err
		}
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return 0, err
	}
	return id, nil
}

const postColumns = `p.id,p.thread_id,p.text,p.created_at,p.removed,p.image_name,p.image_mime,p.image_bytes,p.image_width,p.image_height`

func (s *Store) posts(ctx context.Context, query string, args ...any) ([]Post, error) {
	s, done, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	rows, err := s.readTx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	posts := []Post{}
	for rows.Next() {
		var p Post
		var name string
		var img Attachment
		err = rows.Scan(&p.ID, &p.ThreadID, &p.Text, &p.CreatedAt, &p.Removed, &name, &img.MIME, &img.Bytes, &img.Width, &img.Height)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if name != "" {
			img.URL = "/images/" + name
			p.Image = &img
		}
		p.Permalink = fmt.Sprintf("/posts/%d", p.ID)
		p.APIURL = fmt.Sprintf("/api/posts/%d", p.ID)
		p.References = []int64{}
		p.Backlinks = []int64{}
		posts = append(posts, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range posts {
		p := &posts[i]
		refs, err := s.readTx.QueryContext(ctx, `SELECT source,target FROM refs WHERE source=? OR target=? ORDER BY source,target`, p.ID, p.ID)
		if err != nil {
			return nil, err
		}
		for refs.Next() {
			var source, target int64
			if err = refs.Scan(&source, &target); err != nil {
				refs.Close()
				return nil, err
			}
			if source == p.ID {
				p.References = append(p.References, target)
			}
			if target == p.ID {
				p.Backlinks = append(p.Backlinks, source)
			}
		}
		err = refs.Err()
		refs.Close()
		if err != nil {
			return nil, err
		}
	}
	return posts, nil
}

func (s *Store) post(ctx context.Context, id int64) (Post, error) {
	ps, err := s.posts(ctx, `SELECT `+postColumns+` FROM posts p WHERE p.id=?`, id)
	if err != nil {
		return Post{}, err
	}
	if len(ps) == 0 {
		return Post{}, errNotFound
	}
	return ps[0], nil
}

func (s *Store) threadMeta(ctx context.Context, id int64) (Thread, error) {
	var t Thread
	err := s.queryRow(ctx, `SELECT id,post_count,last_post_id,bumped_at FROM threads WHERE id=?`, id).Scan(&t.ID, &t.PostCount, &t.LastPostID, &t.BumpedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, errNotFound
	}
	if err != nil {
		return t, err
	}
	t.PostLimit = postLimit
	t.Full = t.PostCount >= postLimit
	t.Permalink = fmt.Sprintf("/threads/%d", id)
	t.APIURL = fmt.Sprintf("/api/threads/%d", id)
	return t, nil
}

func (s *Store) thread(ctx context.Context, id int64) (Thread, error) {
	s, done, err := s.snapshot(ctx)
	if err != nil {
		return Thread{}, err
	}
	defer done()
	t, err := s.threadMeta(ctx, id)
	if err != nil {
		return t, err
	}
	t.Posts, err = s.posts(ctx, `SELECT `+postColumns+` FROM posts p WHERE p.thread_id=? ORDER BY p.id`, id)
	return t, err
}

func (s *Store) list(ctx context.Context, page int) ([]Thread, bool, error) {
	s, done, err := s.snapshot(ctx)
	if err != nil {
		return nil, false, err
	}
	defer done()
	rows, err := s.readTx.QueryContext(ctx, `SELECT id FROM threads ORDER BY bumped_at DESC,last_post_id DESC LIMIT ? OFFSET ?`, pageSize+1, (page-1)*pageSize)
	if err != nil {
		return nil, false, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	more := len(ids) > pageSize
	if more {
		ids = ids[:pageSize]
	}
	ts := []Thread{}
	for _, id := range ids {
		t, err := s.threadMeta(ctx, id)
		if err != nil {
			return nil, false, err
		}
		p, err := s.post(ctx, id)
		if err != nil {
			return nil, false, err
		}
		shorten(&p)
		t.Posts = []Post{p}
		ts = append(ts, t)
	}
	return ts, more, nil
}

func (s *Store) postContext(ctx context.Context, id int64) (Post, Thread, error) {
	s, done, err := s.snapshot(ctx)
	if err != nil {
		return Post{}, Thread{}, err
	}
	defer done()
	p, err := s.post(ctx, id)
	if err != nil {
		return Post{}, Thread{}, err
	}
	t, err := s.threadMeta(ctx, p.ThreadID)
	return p, t, err
}

func (s *Store) search(ctx context.Context, q string, page int) ([]Post, bool, error) {
	terms := strings.Fields(q)
	if len(terms) == 0 {
		return []Post{}, false, nil
	}
	for i, t := range terms {
		terms[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	ps, err := s.posts(ctx, `SELECT `+postColumns+` FROM posts_fts JOIN posts p ON p.id=posts_fts.rowid WHERE posts_fts MATCH ? AND p.removed=0 ORDER BY rank,p.id DESC LIMIT ? OFFSET ?`, strings.Join(terms, " AND "), pageSize+1, (page-1)*pageSize)
	if err != nil {
		return nil, false, err
	}
	more := len(ps) > pageSize
	if more {
		ps = ps[:pageSize]
	}
	for i := range ps {
		shorten(&ps[i])
	}
	return ps, more, nil
}

func shorten(p *Post) {
	r := []rune(p.Text)
	if len(r) > previewLimit {
		p.Text = string(r[:previewLimit])
		p.Truncated = true
	}
}

func (s *Store) remove(ctx context.Context, id int64, imageOnly bool) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	var name string
	err = conn.QueryRowContext(ctx, `SELECT image_name FROM posts WHERE id=?`, id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	query := `UPDATE posts SET image_name='',image_mime='',image_bytes=0,image_width=0,image_height=0`
	if !imageOnly {
		query += `,text='',removed=1`
	}
	if _, err = conn.ExecContext(ctx, query+` WHERE id=?`, id); err != nil {
		return err
	}
	// Remove outgoing edges with removed content; incoming links still reach the tombstone.
	if !imageOnly {
		if _, err = conn.ExecContext(ctx, `DELETE FROM refs WHERE source=?`, id); err != nil {
			return err
		}
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	if name != "" {
		if err = os.Remove(filepath.Join(s.dir, "images", name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
