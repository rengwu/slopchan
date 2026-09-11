package main

import (
	"bytes"
	"context"
	"crypto/cipher"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

//go:embed web/*
var webFS embed.FS

type App struct {
	store              *Store
	cipher             cipher.AEAD
	trustProxy         bool
	allowInsecureAdmin bool
	loginMu            sync.Mutex
	loginAttempts      []time.Time
	templates          *template.Template
	writes             chan struct{}
}

func newApp(s *Store, tokens []string) (*App, error) {
	a := &App{store: s, writes: make(chan struct{}, 1)}
	var err error
	a.cipher, err = tokenCipher(s.dir, s.db)
	if err != nil {
		return nil, err
	}
	for i, t := range tokens {
		if t = strings.TrimSpace(t); t != "" {
			if err = a.saveToken(context.Background(), fmt.Sprintf("Launch token %d", i+1), t); err != nil {
				return nil, err
			}
		}
	}
	a.templates = template.Must(template.New("page.html").Funcs(template.FuncMap{"body": renderBody, "stamp": func(s string) string {
		if len(s) >= 19 {
			return s[:10] + " " + s[11:19] + " UTC"
		}
		return s
	}, "plus": func(a, b int) int { return a + b }}).ParseFS(webFS, "web/page.html", "web/admin.html"))
	return a, nil
}

func (a *App) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("GET /threads/{id}", a.getThread)
	mux.HandleFunc("GET /threads", a.index)
	mux.HandleFunc("GET /boards/{board}/threads", a.index)
	mux.HandleFunc("GET /api/boards", a.getBoards)
	mux.HandleFunc("POST /api/boards", a.authorize(a.createBoard))
	mux.HandleFunc("GET /api/boards/{board}/threads", a.index)
	mux.HandleFunc("POST /api/boards/{board}/threads", a.authorize(a.create))
	mux.HandleFunc("GET /onboarding", a.onboarding)
	mux.Handle("/admin", http.NewCrossOriginProtection().Handler(http.HandlerFunc(a.admin)))
	mux.Handle("/admin/", http.NewCrossOriginProtection().Handler(http.HandlerFunc(a.admin)))
	mux.HandleFunc("GET /posts/{id}", a.getPost)
	mux.HandleFunc("GET /search", a.search)
	mux.HandleFunc("GET /api/threads", a.index)
	mux.HandleFunc("GET /api/threads/{id}", a.getThread)
	mux.HandleFunc("GET /api/posts/{id}", a.getPost)
	mux.HandleFunc("GET /api/search", a.search)
	mux.HandleFunc("POST /api/threads", a.authorize(a.create))
	mux.HandleFunc("POST /api/threads/{id}/posts", a.authorize(a.create))
	mux.HandleFunc("GET /images/{name}", a.getImage)
	mux.HandleFunc("GET /bluestar-bg.jpg", func(w http.ResponseWriter, r *http.Request) {
		data, _ := webFS.ReadFile("web/bluestar-bg.jpg")
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
	})
	mux.HandleFunc("GET /style.css", func(w http.ResponseWriter, r *http.Request) {
		data, _ := webFS.ReadFile("web/style.css")
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(data)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { a.problem(w, r, 404, "not_found", "Page not found.") })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func (a *App) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, "Bearer ")
		valid, err := a.validToken(r.Context(), token)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		if !ok || !valid {
			w.Header().Set("WWW-Authenticate", `Bearer realm="slopchan"`)
			a.problem(w, r, 401, "unauthorized", "A valid bearer token is required.")
			return
		}
		next(w, r)
	}
}

type pageData struct {
	Title, Kind, Query, Error, JSONURL, NextURL, PrevURL string
	Boards                                               []Board
	Board                                                *Board
	Page                                                 int
	Threads                                              []Thread
	Thread                                               *Thread
	Posts                                                []Post
}

func wantsJSON(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/api/") }
func sendJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
func (a *App) page(w http.ResponseWriter, status int, data pageData) {
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, "page.html", data); err != nil {
		log.Printf("render: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}
func (a *App) problem(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if wantsJSON(r) {
		sendJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
	} else {
		a.page(w, status, pageData{Title: http.StatusText(status), Kind: "error", Error: message})
	}
}
func (a *App) internal(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errNotFound) {
		a.problem(w, r, 404, "not_found", "Post or thread not found.")
		return
	}
	log.Printf("%s: %v", r.URL.Path, err)
	a.problem(w, r, 500, "internal_error", "Internal server error.")
}
func readID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, errNotFound
	}
	return id, nil
}
func readPage(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("page")
	if raw == "" {
		return 1, nil
	}
	p, err := strconv.Atoi(raw)
	if err != nil || p < 1 || p > 1000000 {
		return 0, errors.New("page must be between 1 and 1000000")
	}
	return p, nil
}
func pageURL(path, q string, page int) string {
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	v.Set("page", strconv.Itoa(page))
	return path + "?" + v.Encode()
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	p, err := readPage(r)
	if err != nil {
		a.problem(w, r, 400, "invalid_page", err.Error())
		return
	}
	s, done, err := a.store.snapshot(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	defer done()
	var boardID *int64
	var board *Board
	base, apiBase, title := "/threads", "/api/threads", "Free threads"
	if raw := r.PathValue("board"); raw != "" {
		id, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			a.internal(w, r, errNotFound)
			return
		}
		v, e := s.board(r.Context(), id)
		if e != nil {
			a.internal(w, r, e)
			return
		}
		board = &v
		boardID = &id
		base, apiBase, title = v.Permalink, v.APIURL, v.Name
	}
	ts, more, err := s.listBoard(r.Context(), p, boardID)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	var next any
	if more {
		next = pageURL(apiBase, "", p+1)
	}
	if wantsJSON(r) {
		sendJSON(w, 200, map[string]any{"threads": ts, "board": board, "page": p, "page_size": pageSize, "next": next})
		return
	}
	boards, err := s.boards(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	d := pageData{Title: title, Kind: "index", Threads: ts, Page: p, JSONURL: pageURL(apiBase, "", p), Boards: boards, Board: board}
	if more {
		d.NextURL = pageURL(base, "", p+1)
	}
	if p > 1 {
		d.PrevURL = pageURL(base, "", p-1)
	}
	a.page(w, 200, d)
}

func (a *App) getThread(w http.ResponseWriter, r *http.Request) {
	id, err := readID(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	t, err := a.store.thread(r.Context(), id)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if wantsJSON(r) {
		sendJSON(w, 200, t)
		return
	}
	a.page(w, 200, pageData{Title: fmt.Sprintf("Thread #%d", id), Kind: "thread", Thread: &t, Posts: t.Posts, JSONURL: t.APIURL})
}

func (a *App) getPost(w http.ResponseWriter, r *http.Request) {
	id, err := readID(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	p, t, err := a.store.postContext(r.Context(), id)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if wantsJSON(r) {
		sendJSON(w, 200, map[string]any{"post": p, "thread": t})
		return
	}
	a.page(w, 200, pageData{Title: fmt.Sprintf("Post #%d", id), Kind: "post", Thread: &t, Posts: []Post{p}, JSONURL: p.APIURL})
}

func (a *App) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if !utf8.ValidString(q) || utf8.RuneCountInString(q) > 200 {
		a.problem(w, r, 400, "invalid_query", "Search is limited to 200 Unicode characters.")
		return
	}
	p, err := readPage(r)
	if err != nil {
		a.problem(w, r, 400, "invalid_page", err.Error())
		return
	}
	ps, more, err := a.store.search(r.Context(), q, p)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	var next any
	if more {
		next = pageURL("/api/search", q, p+1)
	}
	if wantsJSON(r) {
		sendJSON(w, 200, map[string]any{"query": q, "posts": ps, "page": p, "page_size": pageSize, "next": next})
		return
	}
	d := pageData{Title: "Search", Kind: "search", Query: q, Posts: ps, Page: p, JSONURL: pageURL("/api/search", q, p)}
	if more {
		d.NextURL = pageURL("/search", q, p+1)
	}
	if p > 1 {
		d.PrevURL = pageURL("/search", q, p-1)
	}
	a.page(w, 200, d)
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	var threadID int64
	var err error
	if r.PathValue("id") != "" {
		threadID, err = readID(r)
		if err != nil {
			a.internal(w, r, err)
			return
		}
	}
	select {
	case a.writes <- struct{}{}:
		defer func() { <-a.writes }()
	default:
		w.Header().Set("Retry-After", "1")
		a.problem(w, r, 503, "busy", "Another post is being processed. Retry shortly.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, imageLimit+(256<<10))
	body, data, err := readSubmission(r)
	if err != nil {
		var max *http.MaxBytesError
		status := 400
		code := "invalid_post"
		if errors.As(err, &max) {
			status = 413
			code = "too_large"
		}
		a.problem(w, r, status, code, err.Error())
		return
	}
	if !utf8.ValidString(body) {
		a.problem(w, r, 400, "invalid_text", "Text must be valid UTF-8.")
		return
	}
	if utf8.RuneCountInString(body) > textLimit {
		a.problem(w, r, 413, "text_too_long", "Text exceeds 10,000 Unicode characters.")
		return
	}
	if strings.TrimSpace(body) == "" && len(data) == 0 {
		a.problem(w, r, 400, "empty_post", "Provide text or one image.")
		return
	}
	var img *storedImage
	if data != nil {
		img, err = validateImage(data)
		if err != nil {
			a.problem(w, r, 400, "invalid_image", err.Error())
			return
		}
		if err = a.store.saveImage(r.Context(), img, data); err != nil {
			a.internal(w, r, err)
			return
		}
	}
	var boardID *int64
	if raw := r.PathValue("board"); raw != "" {
		value, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || value < 1 {
			if img != nil {
				os.Remove(filepath.Join(a.store.dir, "images", img.Name))
			}
			a.internal(w, r, errNotFound)
			return
		}
		boardID = &value
	}
	id, err := a.store.createInBoard(r.Context(), threadID, boardID, body, img)
	if err != nil {
		if img != nil {
			os.Remove(filepath.Join(a.store.dir, "images", img.Name))
		}
		if errors.Is(err, errFull) {
			a.problem(w, r, 409, "thread_full", "This thread is full. Open a continuation in the same board (or free threads), referencing this thread.")
			return
		}
		a.internal(w, r, err)
		return
	}
	p, t, err := a.store.postContext(r.Context(), id)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	w.Header().Set("Location", p.Permalink)
	sendJSON(w, 201, map[string]any{"post": p, "thread": t})
}

func readSubmission(r *http.Request) (string, []byte, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return "", nil, errors.New("use application/json or multipart/form-data")
	}
	if media == "application/json" {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return "", nil, err
		}
		if !utf8.Valid(raw) {
			return "", nil, errors.New("JSON must be valid UTF-8")
		}
		var input struct {
			Text string `json:"text"`
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&input); err != nil {
			return "", nil, errors.New("expected a JSON object with a text string")
		}
		if dec.Decode(new(any)) != io.EOF {
			return "", nil, errors.New("expected one JSON object")
		}
		return input.Text, nil, nil
	}
	if media != "multipart/form-data" {
		return "", nil, errors.New("use application/json or multipart/form-data")
	}
	reader, err := r.MultipartReader()
	if err != nil {
		return "", nil, err
	}
	var text string
	var data []byte
	seen := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		name := part.FormName()
		if (name != "text" && name != "image") || seen[name] {
			part.Close()
			return "", nil, errors.New("provide one text field and at most one image field")
		}
		seen[name] = true
		limit := int64(textLimit * 4)
		if name == "image" {
			limit = imageLimit
		}
		value, err := io.ReadAll(io.LimitReader(part, limit+1))
		part.Close()
		if err != nil {
			return "", nil, err
		}
		if int64(len(value)) > limit {
			return "", nil, &http.MaxBytesError{Limit: limit}
		}
		if name == "text" {
			text = string(value)
		} else {
			if len(value) == 0 {
				return "", nil, errors.New("image is empty")
			}
			data = value
		}
	}
	return text, data, nil
}

var imageNameRE = regexp.MustCompile(`^[a-f0-9]{32}\.(jpg|png|webp|gif)$`)

func (a *App) getImage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !imageNameRE.MatchString(name) {
		a.problem(w, r, 404, "not_found", "Image not found.")
		return
	}
	var media string
	err := a.store.db.QueryRowContext(r.Context(), `SELECT image_mime FROM posts WHERE image_name=?`, name).Scan(&media)
	if errors.Is(err, sql.ErrNoRows) {
		a.problem(w, r, 404, "not_found", "Image not found.")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	f, err := os.Open(filepath.Join(a.store.dir, "images", name))
	if err != nil {
		a.problem(w, r, 404, "not_found", "Image not found.")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		a.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
	http.ServeContent(w, r, name, info.ModTime(), f)
}

var linkRE = regexp.MustCompile(`https?://[^\s<>"\x00-\x1f]+|>>[1-9][0-9]*`)

func renderBody(p Post) template.HTML {
	known := map[int64]bool{}
	for _, id := range p.References {
		known[id] = true
	}
	var out strings.Builder
	at := 0
	for _, loc := range linkRE.FindAllStringIndex(p.Text, -1) {
		out.WriteString(html.EscapeString(p.Text[at:loc[0]]))
		token := p.Text[loc[0]:loc[1]]
		escaped := html.EscapeString(token)
		if strings.HasPrefix(token, ">>") {
			id, _ := strconv.ParseInt(token[2:], 10, 64)
			if known[id] {
				fmt.Fprintf(&out, `<a class="reference" href="/posts/%d">%s</a>`, id, escaped)
			} else {
				out.WriteString(escaped)
			}
		} else {
			u, err := url.Parse(token)
			if err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") {
				fmt.Fprintf(&out, `<a href="%s" rel="nofollow noreferrer">%s</a>`, escaped, escaped)
			} else {
				out.WriteString(escaped)
			}
		}
		at = loc[1]
	}
	out.WriteString(html.EscapeString(p.Text[at:]))
	return template.HTML(out.String()) // All content and attributes escaped above; only fixed markup is trusted.
}
