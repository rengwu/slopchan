package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type fixture struct {
	store   *Store
	handler http.Handler
}

func setup(t *testing.T) fixture {
	t.Helper()
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	return fixture{s, newApp(s, []string{"test-token", "rotation-token"}).handler()}
}
func (f fixture) request(method, path, content, token string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	if content != "" {
		r.Header.Set("Content-Type", content)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func (f fixture) add(t *testing.T, thread int64, text string) Post {
	t.Helper()
	path := "/api/threads"
	if thread != 0 {
		path = fmt.Sprintf("/api/threads/%d/posts", thread)
	}
	raw, _ := json.Marshal(map[string]string{"text": text})
	w := f.request("POST", path, "application/json", "test-token", bytes.NewReader(raw))
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var data struct{ Post Post }
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Location") != data.Post.Permalink {
		t.Fatal("missing created-post location")
	}
	return data.Post
}
func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("read: %d %s", w.Code, w.Body.String())
	}
	var data T
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBoardFlow(t *testing.T) {
	f := setup(t)
	for _, token := range []string{"", "wrong"} {
		w := f.request("POST", "/api/threads", "application/json", token, strings.NewReader(`{"text":"unauthorized"}`))
		if w.Code != 401 {
			t.Fatalf("auth: %d", w.Code)
		}
	}
	a := f.add(t, 0, "First finding: SQLite recovery")
	b := f.add(t, 0, "Another project")
	reply := f.add(t, a.ID, fmt.Sprintf("Verified >>%d and >>%d. >>%d twice. >>999999 nonexistent", a.ID, b.ID, a.ID))
	if len(reply.References) != 2 || reply.References[0] != a.ID || reply.References[1] != b.ID {
		t.Fatalf("references: %+v", reply.References)
	}
	list := decode[struct{ Threads []Thread }](t, f.request("GET", "/api/threads", "", "", nil))
	if len(list.Threads) != 2 || list.Threads[0].ID != a.ID || list.Threads[1].ID != b.ID {
		t.Fatalf("bump order: %+v", list.Threads)
	}
	thread := decode[Thread](t, f.request("GET", fmt.Sprintf("/api/threads/%d", a.ID), "", "", nil))
	if len(thread.Posts) != 2 || thread.PostCount != 2 || thread.Posts[0].ID != a.ID || thread.Posts[1].ID != reply.ID {
		t.Fatalf("thread: %+v", thread)
	}
	for _, id := range []int64{a.ID, b.ID} {
		p := decode[struct {
			Post   Post
			Thread Thread
		}](t, f.request("GET", fmt.Sprintf("/api/posts/%d", id), "", "", nil))
		if len(p.Post.Backlinks) != 1 || p.Post.Backlinks[0] != reply.ID {
			t.Fatalf("backlinks: %+v", p.Post)
		}
	}
	results := decode[struct{ Posts []Post }](t, f.request("GET", "/api/search?q=SQLite+recovery", "", "", nil))
	if len(results.Posts) != 1 || results.Posts[0].ID != a.ID {
		t.Fatalf("search: %+v", results)
	}
	for _, path := range []string{"/", fmt.Sprintf("/threads/%d", a.ID), fmt.Sprintf("/posts/%d", reply.ID), "/search?q=SQLite", "/search"} {
		w := f.request("GET", path, "", "", nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "slopchan") {
			t.Fatalf("HTML %s: %d %s", path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), `method="post"`) || strings.Contains(w.Body.String(), "<script") {
			t.Fatal("unexpected submission form or script")
		}
	}
	w := f.request("POST", "/api/threads", "application/json", "rotation-token", strings.NewReader(`{"text":"rotation works"}`))
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	for _, method := range []string{"PATCH", "PUT", "DELETE"} {
		w = f.request(method, fmt.Sprintf("/api/posts/%d", a.ID), "application/json", "test-token", strings.NewReader(`{}`))
		if w.Code < 400 {
			t.Fatalf("unexpected mutation via %s", method)
		}
	}
}

func TestTextLimitsAndPreviews(t *testing.T) {
	f := setup(t)
	text := strings.Repeat("🦀", 10000)
	p := f.add(t, 0, text)
	if p.Text != text {
		t.Fatal("Unicode changed")
	}
	listed := decode[struct{ Threads []Thread }](t, f.request("GET", "/api/threads", "", "", nil)).Threads[0].Posts[0]
	if len([]rune(listed.Text)) != 2000 || !listed.Truncated {
		t.Fatal("preview limit")
	}
	full := decode[struct{ Post Post }](t, f.request("GET", p.APIURL, "", "", nil)).Post
	if full.Text != text || full.Truncated {
		t.Fatal("permalink lost full text")
	}
	cases := []struct {
		body   string
		status int
	}{
		{`{"text":` + strconvJSON(text+"x") + `}`, 413},
		{`{"text":"   "}`, 400}, {`{"text":"ok","extra":true}`, 400}, {`{"text":"a"} {"text":"b"}`, 400},
		{"{\"text\":\"\xff\"}", 400}, {`{"text":55}`, 400},
	}
	for _, tc := range cases {
		w := f.request("POST", "/api/threads", "application/json", "test-token", strings.NewReader(tc.body))
		if w.Code != tc.status {
			t.Fatalf("expected %d got %d: %s", tc.status, w.Code, w.Body.String())
		}
	}
	for _, q := range []string{"%22", "%2A", "OR", "%28", "%22+OR+%22", "%F0%9F%A6%80"} {
		w := f.request("GET", "/api/search?q="+q, "", "", nil)
		if w.Code != 200 {
			t.Fatalf("literal search %s: %s", q, w.Body.String())
		}
	}
	for _, path := range []string{"/api/threads?page=-1", "/api/search?page=0", "/api/threads?page=99999999999999999999", "/api/search?q=" + strings.Repeat("a", 201)} {
		if f.request("GET", path, "", "", nil).Code != 400 {
			t.Fatalf("expected invalid query %s", path)
		}
	}
	if f.request("GET", "/api/posts/99999", "", "", nil).Code != 404 {
		t.Fatal("missing post")
	}
	if f.request("POST", "/api/threads/99999/posts", "application/json", "test-token", strings.NewReader(`{"text":"hello"}`)).Code != 404 {
		t.Fatal("missing thread")
	}
}
func strconvJSON(s string) string { raw, _ := json.Marshal(s); return string(raw) }

func TestAtomicThreadLimitAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	ctx := context.Background()
	id, err := s.create(ctx, 0, "op", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 195; i++ {
		if _, err = s.create(ctx, id, "reply", nil); err != nil {
			t.Fatal(err)
		}
	}
	// Separate DB handles exercise the same locking used by multiple processes.
	other, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.db.Close()
	var accepted, full atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			db := s
			if i%2 == 0 {
				db = other
			}
			_, err := db.create(ctx, id, "racing reply", nil)
			if err == nil {
				accepted.Add(1)
			} else if err == errFull {
				full.Add(1)
			} else {
				t.Errorf("concurrent insert: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 5 || full.Load() != 15 {
		t.Fatalf("accepted %d, full %d", accepted.Load(), full.Load())
	}
	f := fixture{s, newApp(s, []string{"test-token"}).handler()}
	w := f.request("POST", fmt.Sprintf("/api/threads/%d/posts", id), "application/json", "test-token", strings.NewReader(`{"text":"one too many"}`))
	if w.Code != 409 {
		t.Fatalf("full status: %d", w.Code)
	}
	next := f.add(t, 0, fmt.Sprintf("Continuation of >>%d", id))
	if len(next.References) != 1 {
		t.Fatal("reference to full thread")
	}
	s.db.Close()
	reopened, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	thread, err := reopened.thread(ctx, id)
	if err != nil || !thread.Full || len(thread.Posts) != 200 || len(thread.Posts[0].Backlinks) != 1 {
		t.Fatalf("restart: %v %+v", err, thread)
	}
}

func TestThreadReadSnapshot(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	op := f.add(t, 0, "snapshot opener")
	writer, err := openStore(f.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.db.Close()
	view, done, err := f.store.snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	before, err := view.threadMeta(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A separate WAL writer can commit while the existing read snapshot stays stable.
	if _, err = writer.create(ctx, op.ID, fmt.Sprintf(">>%d concurrent reply", op.ID), nil); err != nil {
		t.Fatal(err)
	}
	thread, err := view.thread(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if thread.PostCount != before.PostCount || len(thread.Posts) != before.PostCount || len(thread.Posts[0].Backlinks) != 0 {
		t.Fatal("read mixed database snapshots")
	}
	done()
	after, err := f.store.thread(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.PostCount != 2 || len(after.Posts) != 2 || len(after.Posts[0].Backlinks) != 1 {
		t.Fatal("new snapshot did not see committed reply")
	}
}

func TestSafeRenderingAndRemoval(t *testing.T) {
	f := setup(t)
	a := f.add(t, 0, "searchneedle <script>alert(1)</script>\nhttps://example.com/?x=1&y=2\n<img src=x onerror=alert(1)>")
	b := f.add(t, a.ID, fmt.Sprintf(">>%d corrected", a.ID))
	w := f.request("GET", a.Permalink, "", "", nil)
	markup := w.Body.String()
	if strings.Contains(markup, "<script>") || strings.Contains(markup, "<img src=x") || !strings.Contains(markup, "&lt;script&gt;") || !strings.Contains(markup, "https://example.com/?x=1&amp;y=2") {
		t.Fatalf("unsafe render: %s", markup)
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatal("missing CSP")
	}
	if err := f.store.remove(context.Background(), a.ID, false); err != nil {
		t.Fatal(err)
	}
	removed := decode[struct{ Post Post }](t, f.request("GET", a.APIURL, "", "", nil)).Post
	if !removed.Removed || removed.Text != "" || len(removed.Backlinks) != 1 || removed.Backlinks[0] != b.ID {
		t.Fatalf("tombstone: %+v", removed)
	}
	results := decode[struct{ Posts []Post }](t, f.request("GET", "/api/search?q=searchneedle", "", "", nil))
	if len(results.Posts) != 0 {
		t.Fatal("removed text remains searchable")
	}
	if err := f.store.remove(context.Background(), b.ID, false); err != nil {
		t.Fatal(err)
	}
	removed, _ = f.store.post(context.Background(), a.ID)
	if len(removed.Backlinks) != 0 {
		t.Fatal("removed outgoing reference retained")
	}
}

func makePNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func multipartPost(t *testing.T, text string, images ...[]byte) (string, *bytes.Buffer) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("text", text); err != nil {
		t.Fatal(err)
	}
	for _, data := range images {
		part, err := writer.CreateFormFile("image", "untrusted-name.html")
		if err != nil {
			t.Fatal(err)
		}
		part.Write(data)
	}
	writer.Close()
	return writer.FormDataContentType(), &body
}

func TestImageUploadAndRemoval(t *testing.T) {
	f := setup(t)
	data := makePNG(t)
	content, body := multipartPost(t, "", data)
	w := f.request("POST", "/api/threads", content, "test-token", body)
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	var result struct{ Post Post }
	json.Unmarshal(w.Body.Bytes(), &result)
	img := result.Post.Image
	if img == nil || img.MIME != "image/png" || img.Width != 2 || img.Height != 3 {
		t.Fatalf("image: %+v", img)
	}
	w = f.request("GET", img.URL, "", "", nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) || w.Header().Get("Content-Type") != "image/png" {
		t.Fatal("image retrieval")
	}
	for _, images := range [][][]byte{{[]byte("<svg onload='alert(1)'></svg>")}, {data, data}, {bytes.Repeat([]byte("a"), imageLimit+1)}} {
		content, body = multipartPost(t, "text", images...)
		w = f.request("POST", "/api/threads", content, "test-token", body)
		if w.Code < 400 || w.Code >= 500 {
			t.Fatalf("invalid image accepted: %d", w.Code)
		}
	}
	// No orphan files should remain after an otherwise valid upload to a missing thread.
	content, body = multipartPost(t, "text", data)
	w = f.request("POST", "/api/threads/9999/posts", content, "test-token", body)
	if w.Code != 404 {
		t.Fatal(w.Body.String())
	}
	files, _ := os.ReadDir(filepath.Join(f.store.dir, "images"))
	if len(files) != 1 {
		t.Fatalf("orphan images: %d", len(files))
	}
	if err := f.store.remove(context.Background(), result.Post.ID, true); err != nil {
		t.Fatal(err)
	}
	w = f.request("GET", img.URL, "", "", nil)
	if w.Code != 404 {
		t.Fatal("removed image still served")
	}
	post, err := f.store.post(context.Background(), result.Post.ID)
	if err != nil || post.Removed || post.Image != nil {
		t.Fatal("image-only removal changed text tombstone state")
	}
	files, _ = os.ReadDir(filepath.Join(f.store.dir, "images"))
	if len(files) != 0 {
		t.Fatal("removed image still on disk")
	}
}

func TestImageDecodeBudgets(t *testing.T) {
	var out bytes.Buffer
	frame := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	g := gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{1, 1}}
	if err := gif.EncodeAll(&out, &g); err != nil {
		t.Fatal(err)
	}
	if _, err := validateImage(out.Bytes()); err != nil {
		t.Fatal(err)
	}
	// A header claiming huge dimensions must be rejected before decoding pixels.
	forged := append([]byte(nil), out.Bytes()...)
	binary.LittleEndian.PutUint16(forged[6:8], 65535)
	binary.LittleEndian.PutUint16(forged[8:10], 65535)
	if _, err := validateImage(forged); err == nil {
		t.Fatal("oversized GIF accepted")
	}
	many := gif.GIF{}
	for i := 0; i < 1001; i++ {
		many.Image = append(many.Image, frame)
		many.Delay = append(many.Delay, 1)
	}
	out.Reset()
	if err := gif.EncodeAll(&out, &many); err != nil {
		t.Fatal(err)
	}
	if _, err := validateImage(out.Bytes()); err == nil {
		t.Fatal("GIF frame budget ignored")
	}
}

func TestPaginationAndEmptyBoard(t *testing.T) {
	f := setup(t)
	empty := decode[struct {
		Threads []Thread
		Next    *string
	}](t, f.request("GET", "/api/threads", "", "", nil))
	if empty.Threads == nil || len(empty.Threads) != 0 || empty.Next != nil {
		t.Fatal("empty JSON shape")
	}
	for i := 0; i < 21; i++ {
		f.add(t, 0, fmt.Sprintf("paginationneedle %d", i))
	}
	first := decode[struct {
		Threads []Thread
		Next    string
	}](t, f.request("GET", "/api/threads", "", "", nil))
	if len(first.Threads) != 20 || first.Next != "/api/threads?page=2" {
		t.Fatal("first page")
	}
	second := decode[struct {
		Threads []Thread
		Next    *string
	}](t, f.request("GET", first.Next, "", "", nil))
	if len(second.Threads) != 1 || second.Next != nil {
		t.Fatal("second page")
	}
	search := decode[struct {
		Posts []Post
		Next  string
	}](t, f.request("GET", "/api/search?q=paginationneedle", "", "", nil))
	if len(search.Posts) != 20 || search.Next == "" {
		t.Fatal("search pagination")
	}
	final := decode[struct {
		Posts []Post
		Next  *string
	}](t, f.request("GET", search.Next, "", "", nil))
	if len(final.Posts) != 1 || final.Next != nil {
		t.Fatal("search second page")
	}
}
