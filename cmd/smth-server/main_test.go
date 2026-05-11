package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactLifecycle(t *testing.T) {
	t.Parallel()

	s := testServer(t, config{
		dataDir:    t.TempDir(),
		staticDir:  "./static",
		publicRead: false,
		maxSize:    defaultMaxSize,
		apiKey:     "secret",
		port:       8080,
	})

	body := `{"html":"<!doctype html><title>From title</title><h1>Hello</h1>","project":"demo","tags":["plan","plan"," draft "]}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/artifacts", strings.NewReader(body))
	createReq.Header.Set("X-API-Key", "secret")
	createRec := httptest.NewRecorder()
	s.routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", createRec.Code, createRec.Body.String())
	}

	var created createResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !validULID(created.ID) {
		t.Fatalf("invalid id: %q", created.ID)
	}

	meta := readMeta(t, s, created.ID)
	if meta.Title != "From title" {
		t.Fatalf("title = %q", meta.Title)
	}
	if meta.Project != "demo" {
		t.Fatalf("project = %q", meta.Project)
	}
	if got := strings.Join(meta.Tags, ","); got != "plan,draft" {
		t.Fatalf("tags = %q", got)
	}

	dateDir := filepath.Join(s.cfg.dataDir, meta.CreatedAt[:10])
	assertFile(t, filepath.Join(dateDir, created.ID+".html"))
	assertFile(t, filepath.Join(dateDir, created.ID+".json"))

	listReq := httptest.NewRequest(http.MethodGet, "/api/artifacts?limit=1", nil)
	listReq.Header.Set("X-API-Key", "secret")
	listRec := httptest.NewRecorder()
	s.routes().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", listRec.Code, listRec.Body.String())
	}
	var list listResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("unexpected list: %+v", list.Items)
	}

	rawReq := httptest.NewRequest(http.MethodGet, "/a/"+created.ID, nil)
	rawReq.Header.Set("X-API-Key", "secret")
	rawRec := httptest.NewRecorder()
	s.routes().ServeHTTP(rawRec, rawReq)
	if rawRec.Code != http.StatusOK {
		t.Fatalf("raw status = %d body = %s", rawRec.Code, rawRec.Body.String())
	}
	if !strings.Contains(rawRec.Body.String(), "<title>From title</title>") {
		t.Fatalf("raw body mismatch: %s", rawRec.Body.String())
	}

	replaceBody := `{"replace":"` + created.ID + `","html":"<h1>Updated</h1>","title":"Updated","tags":["final"]}`
	replaceReq := httptest.NewRequest(http.MethodPost, "/api/artifacts", strings.NewReader(replaceBody))
	replaceReq.Header.Set("X-API-Key", "secret")
	replaceRec := httptest.NewRecorder()
	s.routes().ServeHTTP(replaceRec, replaceReq)
	if replaceRec.Code != http.StatusOK {
		t.Fatalf("replace status = %d body = %s", replaceRec.Code, replaceRec.Body.String())
	}
	meta = readMeta(t, s, created.ID)
	if meta.Title != "Updated" || strings.Join(meta.Tags, ",") != "final" {
		t.Fatalf("replace meta = %+v", meta)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/artifacts/"+created.ID, nil)
	deleteReq.Header.Set("X-API-Key", "secret")
	deleteRec := httptest.NewRecorder()
	s.routes().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body = %s", deleteRec.Code, deleteRec.Body.String())
	}
	meta = readMeta(t, s, created.ID)
	if meta.DeletedAt == "" {
		t.Fatalf("deleted_at not set: %+v", meta)
	}
}

func TestAuthAndMaxSize(t *testing.T) {
	t.Parallel()

	s := testServer(t, config{
		dataDir:   t.TempDir(),
		staticDir: "./static",
		maxSize:   8,
		apiKey:    "secret",
		port:      8080,
	})

	noAuth := httptest.NewRecorder()
	s.routes().ServeHTTP(noAuth, httptest.NewRequest(http.MethodGet, "/api/artifacts", nil))
	if noAuth.Code != http.StatusUnauthorized {
		t.Fatalf("no auth status = %d", noAuth.Code)
	}

	tooLarge := httptest.NewRequest(http.MethodPost, "/api/artifacts", bytes.NewBufferString(`{"html":"123456789"}`))
	tooLarge.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, tooLarge)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestReadAuthRejectsQueryAPIKey(t *testing.T) {
	t.Parallel()

	s := testServer(t, config{
		dataDir:   t.TempDir(),
		staticDir: "./static",
		maxSize:   defaultMaxSize,
		apiKey:    "secret",
		port:      8080,
	})

	meta := artifact{
		ID:        "01K7Z0SP2Q2E6MG5D7TXQW77SB",
		Title:     "secret leak check",
		CreatedAt: "2026-05-11T00:00:00Z",
		UpdatedAt: "2026-05-11T00:00:00Z",
		SizeBytes: 12,
	}
	if err := s.writeArtifact(meta, []byte("<h1>x</h1>")); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/a/"+meta.ID+"?api_key=secret", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("query api_key status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestLogRequestPreservesFlusherForStream(t *testing.T) {
	t.Parallel()

	s := testServer(t, config{
		dataDir:    t.TempDir(),
		staticDir:  "./static",
		publicRead: true,
		maxSize:    defaultMaxSize,
		apiKey:     "secret",
		port:       8080,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/stream", nil)
	rec := httptest.NewRecorder()
	logRequest(s.routes()).ServeHTTP(rec, req)

	if rec.Code == http.StatusInternalServerError {
		t.Fatalf("stream returned 500 through log wrapper: %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestLogRequestRedactsAPIKey(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(oldWriter) })

	handler := logRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/a/01K7Z0SP2Q2E6MG5D7TXQW77SB?api_key=secret&x=1", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	got := out.String()
	if strings.Contains(got, "secret") {
		t.Fatalf("log leaked api key: %s", got)
	}
	if !strings.Contains(got, "api_key=REDACTED") {
		t.Fatalf("log did not redact api key: %s", got)
	}
}

func testServer(t *testing.T, cfg config) *server {
	t.Helper()
	if err := os.MkdirAll(cfg.dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &server{cfg: cfg, broker: newBroker()}
}

func readMeta(t *testing.T, s *server, id string) artifact {
	t.Helper()
	meta, err := s.readArtifact(id)
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func assertFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
}
