package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultPort    = 8080
	defaultDataDir = "./data"
	defaultStatic  = "./static"
	defaultMaxSize = 2 * 1024 * 1024
	eventBacklog   = 100
)

var (
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	h1Re    = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	tagRe   = regexp.MustCompile(`(?is)<[^>]+>`)
)

type config struct {
	port       int
	dataDir    string
	staticDir  string
	publicRead bool
	maxSize    int64
	apiKey     string
	baseURL    string
}

type server struct {
	cfg    config
	broker *broker
}

type artifact struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Project   string   `json:"project,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	SizeBytes int64    `json:"size_bytes"`
	DeletedAt string   `json:"deleted_at,omitempty"`
}

type createRequest struct {
	HTML    string   `json:"html"`
	Title   string   `json:"title"`
	Project string   `json:"project"`
	Tags    []string `json:"tags"`
	Replace string   `json:"replace"`
}

type createResponse struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

type listResponse struct {
	Items      []artifact `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type streamEvent struct {
	ID    string          `json:"id"`
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type subscriber struct {
	ch chan streamEvent
}

type broker struct {
	mu          sync.Mutex
	nextID      uint64
	backlog     []streamEvent
	subscribers map[*subscriber]struct{}
}

func newBroker() *broker {
	return &broker{subscribers: make(map[*subscriber]struct{})}
}

func (b *broker) publish(event string, data any) streamEvent {
	payload, err := json.Marshal(data)
	if err != nil {
		log.Printf("marshal sse event: %v", err)
		payload = []byte(`{}`)
	}

	b.mu.Lock()
	b.nextID++
	ev := streamEvent{
		ID:    newEventID(b.nextID),
		Event: event,
		Data:  payload,
	}
	b.backlog = append(b.backlog, ev)
	if len(b.backlog) > eventBacklog {
		b.backlog = b.backlog[len(b.backlog)-eventBacklog:]
	}
	for sub := range b.subscribers {
		select {
		case sub.ch <- ev:
		default:
			log.Printf("dropped event %s for slow subscriber", ev.ID)
		}
	}
	b.mu.Unlock()

	return ev
}

func (b *broker) subscribe(lastID string) (*subscriber, []streamEvent) {
	sub := &subscriber{ch: make(chan streamEvent, 16)}

	b.mu.Lock()
	replay := replayAfter(b.backlog, lastID)
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()

	return sub, replay
}

func (b *broker) unsubscribe(sub *subscriber) {
	b.mu.Lock()
	delete(b.subscribers, sub)
	close(sub.ch)
	b.mu.Unlock()
}

func replayAfter(events []streamEvent, lastID string) []streamEvent {
	if lastID == "" {
		return nil
	}
	idx := -1
	for i, ev := range events {
		if ev.ID == lastID {
			idx = i
			break
		}
	}
	if idx == -1 || idx == len(events)-1 {
		return nil
	}
	replay := make([]streamEvent, len(events[idx+1:]))
	copy(replay, events[idx+1:])
	return replay
}

func main() {
	cfg := parseConfig()
	if err := os.MkdirAll(cfg.dataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	srv := &server{
		cfg:    cfg,
		broker: newBroker(),
	}

	httpServer := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.port),
		Handler:           logRequest(srv.routes()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("SMTH listening on http://localhost:%d", cfg.port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func parseConfig() config {
	var cfg config
	flag.IntVar(&cfg.port, "port", defaultPort, "HTTP port")
	flag.StringVar(&cfg.dataDir, "data", defaultDataDir, "artifact data directory")
	flag.StringVar(&cfg.staticDir, "static", defaultStatic, "static frontend directory")
	flag.BoolVar(&cfg.publicRead, "public-read", false, "allow unauthenticated read endpoints")
	flag.Int64Var(&cfg.maxSize, "max-size", defaultMaxSize, "maximum HTML size in bytes")
	flag.StringVar(&cfg.baseURL, "base-url", "", "public base URL used in create responses")
	flag.Parse()
	cfg.apiKey = os.Getenv("SMTH_API_KEY")
	return cfg
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/artifacts", s.handleArtifacts)
	mux.HandleFunc("/api/artifacts/", s.handleArtifact)
	mux.HandleFunc("/api/stream", s.handleStream)
	mux.HandleFunc("/a/", s.handleRawArtifact)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(s.cfg.staticDir))))
	return mux
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.cfg.staticDir, "smth.html"))
}

func (s *server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if !s.requireWriteAuth(w, r) {
			return
		}
		s.createArtifact(w, r)
	case http.MethodGet:
		if !s.requireReadAuth(w, r) {
			return
		}
		s.listArtifacts(w, r)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/artifacts/")
	if !validULID(id) {
		http.Error(w, "invalid artifact id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requireReadAuth(w, r) {
			return
		}
		meta, err := s.readArtifact(id)
		if err != nil {
			writeArtifactReadError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, meta)
	case http.MethodDelete:
		if !s.requireWriteAuth(w, r) {
			return
		}
		s.deleteArtifact(w, id)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (s *server) handleRawArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.requireReadAuth(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/a/")
	if !validULID(id) {
		http.Error(w, "invalid artifact id", http.StatusBadRequest)
		return
	}

	meta, err := s.readArtifact(id)
	if err != nil {
		writeArtifactReadError(w, err)
		return
	}
	if meta.DeletedAt != "" {
		http.NotFound(w, r)
		return
	}

	path := s.htmlPath(meta)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, path)
}

func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.requireReadAuth(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	lastID := r.Header.Get("Last-Event-ID")
	sub, replay := s.broker.subscribe(lastID)
	defer s.broker.unsubscribe(sub)

	writer := bufio.NewWriter(w)
	for _, ev := range replay {
		writeSSE(writer, ev)
	}
	writer.Flush()
	flusher.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-sub.ch:
			writeSSE(writer, ev)
			writer.Flush()
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(writer, ": heartbeat\n\n")
			writer.Flush()
			flusher.Flush()
		}
	}
}

func (s *server) createArtifact(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	reader := http.MaxBytesReader(w, r.Body, s.cfg.maxSize+1024*1024)
	if err := json.NewDecoder(reader).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	htmlBytes := []byte(req.HTML)
	if len(htmlBytes) == 0 {
		http.Error(w, "html is required", http.StatusBadRequest)
		return
	}
	if int64(len(htmlBytes)) > s.cfg.maxSize {
		http.Error(w, "html too large", http.StatusRequestEntityTooLarge)
		return
	}

	now := time.Now().UTC()
	if req.Replace != "" {
		if !validULID(req.Replace) {
			http.Error(w, "invalid replace id", http.StatusBadRequest)
			return
		}
		s.replaceArtifact(w, r, req, htmlBytes, now)
		return
	}

	id, err := newULID(now)
	if err != nil {
		http.Error(w, "failed to generate id", http.StatusInternalServerError)
		return
	}
	meta := artifact{
		ID:        id,
		Title:     firstNonEmpty(req.Title, inferTitle(req.HTML, now)),
		Project:   strings.TrimSpace(req.Project),
		Tags:      cleanTags(req.Tags),
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		SizeBytes: int64(len(htmlBytes)),
	}
	if err := s.writeArtifact(meta, htmlBytes); err != nil {
		http.Error(w, "failed to save artifact", http.StatusInternalServerError)
		log.Printf("save artifact %s: %v", id, err)
		return
	}

	s.broker.publish("new", map[string]any{
		"id":         meta.ID,
		"title":      meta.Title,
		"project":    meta.Project,
		"created_at": meta.CreatedAt,
	})

	writeJSON(w, http.StatusOK, createResponse{
		ID:        meta.ID,
		URL:       s.artifactURL(r, meta.ID),
		CreatedAt: meta.CreatedAt,
	})
}

func (s *server) replaceArtifact(w http.ResponseWriter, r *http.Request, req createRequest, htmlBytes []byte, now time.Time) {
	meta, err := s.readArtifact(req.Replace)
	if err != nil {
		writeArtifactReadError(w, err)
		return
	}
	if meta.DeletedAt != "" {
		http.Error(w, "cannot replace deleted artifact", http.StatusGone)
		return
	}

	if strings.TrimSpace(req.Title) != "" {
		meta.Title = strings.TrimSpace(req.Title)
	} else if meta.Title == "" {
		meta.Title = inferTitle(req.HTML, now)
	}
	if strings.TrimSpace(req.Project) != "" {
		meta.Project = strings.TrimSpace(req.Project)
	}
	if req.Tags != nil {
		meta.Tags = cleanTags(req.Tags)
	}
	meta.UpdatedAt = now.Format(time.RFC3339)
	meta.SizeBytes = int64(len(htmlBytes))

	if err := s.writeArtifact(meta, htmlBytes); err != nil {
		http.Error(w, "failed to replace artifact", http.StatusInternalServerError)
		log.Printf("replace artifact %s: %v", meta.ID, err)
		return
	}

	s.broker.publish("update", map[string]any{
		"id":         meta.ID,
		"updated_at": meta.UpdatedAt,
	})

	writeJSON(w, http.StatusOK, createResponse{
		ID:        meta.ID,
		URL:       s.artifactURL(r, meta.ID),
		CreatedAt: meta.CreatedAt,
	})
}

func (s *server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	before := strings.TrimSpace(r.URL.Query().Get("before"))
	limit := parseLimit(r.URL.Query().Get("limit"))

	items, err := s.loadAllMetadata()
	if err != nil {
		http.Error(w, "failed to load artifacts", http.StatusInternalServerError)
		log.Printf("load metadata: %v", err)
		return
	}

	filtered := make([]artifact, 0, len(items))
	for _, item := range items {
		if item.DeletedAt != "" {
			continue
		}
		if project != "" && item.Project != project {
			continue
		}
		if before != "" && item.ID >= before {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID > filtered[j].ID
	})

	resp := listResponse{}
	if len(filtered) > limit {
		resp.Items = filtered[:limit]
		resp.NextCursor = filtered[limit-1].ID
	} else {
		resp.Items = filtered
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) deleteArtifact(w http.ResponseWriter, id string) {
	meta, err := s.readArtifact(id)
	if err != nil {
		writeArtifactReadError(w, err)
		return
	}
	if meta.DeletedAt == "" {
		now := time.Now().UTC().Format(time.RFC3339)
		meta.DeletedAt = now
		meta.UpdatedAt = now
		if err := s.writeMetadata(meta); err != nil {
			http.Error(w, "failed to delete artifact", http.StatusInternalServerError)
			log.Printf("delete artifact %s: %v", id, err)
			return
		}
		s.broker.publish("delete", map[string]any{"id": id})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) writeArtifact(meta artifact, htmlBytes []byte) error {
	dir := s.artifactDir(meta)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmpHTML := filepath.Join(dir, meta.ID+".html.tmp")
	if err := os.WriteFile(tmpHTML, htmlBytes, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpHTML, filepath.Join(dir, meta.ID+".html")); err != nil {
		return err
	}
	return s.writeMetadata(meta)
}

func (s *server) writeMetadata(meta artifact) error {
	dir := s.artifactDir(meta)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tmpJSON := filepath.Join(dir, meta.ID+".json.tmp")
	if err := os.WriteFile(tmpJSON, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpJSON, filepath.Join(dir, meta.ID+".json"))
}

func (s *server) readArtifact(id string) (artifact, error) {
	matches, err := filepath.Glob(filepath.Join(s.cfg.dataDir, "*", id+".json"))
	if err != nil {
		return artifact{}, err
	}
	if len(matches) == 0 {
		return artifact{}, os.ErrNotExist
	}
	if len(matches) > 1 {
		return artifact{}, fmt.Errorf("duplicate artifact id %s", id)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		return artifact{}, err
	}
	var meta artifact
	if err := json.Unmarshal(raw, &meta); err != nil {
		return artifact{}, err
	}
	return meta, nil
}

func (s *server) loadAllMetadata() ([]artifact, error) {
	var items []artifact
	err := filepath.WalkDir(s.cfg.dataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var item artifact
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		items = append(items, item)
		return nil
	})
	return items, err
}

func (s *server) artifactDir(meta artifact) string {
	created, err := time.Parse(time.RFC3339, meta.CreatedAt)
	if err != nil {
		created = time.Now().UTC()
	}
	return filepath.Join(s.cfg.dataDir, created.Format("2006-01-02"))
}

func (s *server) htmlPath(meta artifact) string {
	return filepath.Join(s.artifactDir(meta), meta.ID+".html")
}

func (s *server) artifactURL(r *http.Request, id string) string {
	base := strings.TrimRight(s.cfg.baseURL, "/")
	if base == "" && r != nil {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		host := r.Host
		if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
			host = forwardedHost
		}
		base = scheme + "://" + host
	}
	if base == "" {
		base = "http://localhost:" + strconv.Itoa(s.cfg.port)
	}
	return base + "/a/" + id
}

func (s *server) requireWriteAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.apiKey == "" {
		http.Error(w, "SMTH_API_KEY is required for write endpoints", http.StatusUnauthorized)
		return false
	}
	if constantTimeEqual(r.Header.Get("X-API-Key"), s.cfg.apiKey) {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func (s *server) requireReadAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.publicRead {
		return true
	}
	if s.cfg.apiKey == "" {
		http.Error(w, "SMTH_API_KEY is required for read endpoints unless --public-read is set", http.StatusUnauthorized)
		return false
	}
	if constantTimeEqual(r.Header.Get("X-API-Key"), s.cfg.apiKey) {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func writeArtifactReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, nil)
		return
	}
	http.Error(w, "failed to read artifact", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeSSE(w *bufio.Writer, ev streamEvent) {
	fmt.Fprintf(w, "id: %s\n", ev.ID)
	fmt.Fprintf(w, "event: %s\n", ev.Event)
	for _, line := range bytes.Split(ev.Data, []byte("\n")) {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func parseLimit(raw string) int {
	if raw == "" {
		return 50
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func inferTitle(rawHTML string, now time.Time) string {
	if title := extractText(titleRe, rawHTML); title != "" {
		return title
	}
	if h1 := extractText(h1Re, rawHTML); h1 != "" {
		return h1
	}
	return "untitled-" + now.Format("1504")
}

func extractText(re *regexp.Regexp, raw string) string {
	matches := re.FindStringSubmatch(raw)
	if len(matches) < 2 {
		return ""
	}
	text := tagRe.ReplaceAllString(matches[1], "")
	return strings.Join(strings.Fields(html.UnescapeString(text)), " ")
}

func cleanTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	cleaned := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		cleaned = append(cleaned, tag)
	}
	return cleaned
}

func validULID(id string) bool {
	if len(id) != 26 {
		return false
	}
	for _, ch := range id {
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", ch) {
			return false
		}
	}
	return true
}

func newULID(t time.Time) (string, error) {
	ms := uint64(t.UnixMilli())
	var chars [26]byte
	encoding := "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

	for i := 9; i >= 0; i-- {
		chars[i] = encoding[ms&31]
		ms >>= 5
	}

	var entropy [16]byte
	if _, err := io.ReadFull(rand.Reader, entropy[:]); err != nil {
		return "", err
	}
	entropy[0] &= 0x03
	value := new(big.Int).SetBytes(entropy[:])
	mask := big.NewInt(31)
	for i := 25; i >= 10; i-- {
		chars[i] = encoding[new(big.Int).And(value, mask).Int64()]
		value.Rsh(value, 5)
	}

	return string(chars[:]), nil
}

func newEventID(seq uint64) string {
	now := time.Now().UTC()
	id, err := newULID(now)
	if err == nil {
		return id
	}
	return fmt.Sprintf("%013d%013d", now.UnixMilli(), seq)
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, r)
		log.Printf("%s %s %d %s", r.Method, sanitizedRequestURI(r), lrw.status, time.Since(start).Round(time.Millisecond))
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func sanitizedRequestURI(r *http.Request) string {
	if r.URL == nil {
		return ""
	}
	u := *r.URL
	q := u.Query()
	if q.Has("api_key") {
		q.Set("api_key", "REDACTED")
		u.RawQuery = q.Encode()
	}
	return u.RequestURI()
}
