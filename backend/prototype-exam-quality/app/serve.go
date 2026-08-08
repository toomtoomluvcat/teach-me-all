package app

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"protoexam/examgen"
)

// The web UI is the same pipeline the TUI drives, with the terminal replaced
// by a browser. It owns no generation logic of its own: every endpoint below
// resolves to extractDocument / buildOutline / examgen.GenerateExam, and the
// on-disk cache is shared with the CLI, so a document prepared from the
// terminal opens instantly here and the other way round.

//go:embed all:web
var webAssets embed.FS

type webServer struct {
	base config
	// ctx outlives any one request on purpose: pass 1 runs for minutes and a
	// student who closes the tab must not cancel it. Only server shutdown does.
	ctx context.Context

	mu    sync.Mutex
	jobs  map[string]*webJob
	preps map[string]*preparedDoc
	seq   int
}

// preparedDoc is one document that has been through extraction and pass 1.
// Keeping the chunks alongside the outline is what makes the reading view and
// generation free of any further model calls.
type preparedDoc struct {
	Key      string
	cfg      config
	Doc      docRef
	Outline  *examgen.Outline
	Chunks   []examgen.Chunk
	Provider string
	Model    string
	Embed    string
	Pages    int
	Prepared time.Time
}

func serveWeb(ctx context.Context, cfg config) error {
	s := &webServer{base: cfg, jobs: map[string]*webJob{}, preps: map[string]*preparedDoc{}}

	assets, err := fs.Sub(webAssets, "web")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(assets))
	mux.HandleFunc("GET /api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("GET /api/models", s.handleModels)
	mux.HandleFunc("GET /api/documents", s.handleDocuments)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("POST /api/prepare", s.handlePrepare)
	mux.HandleFunc("POST /api/generate", s.handleGenerate)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /api/jobs/{id}/events", s.handleJobEvents)
	mux.HandleFunc("GET /api/prep/{key}", s.handlePrep)
	mux.HandleFunc("GET /api/prep/{key}/lesson/{lesson}", s.handleLesson)

	srv := &http.Server{Addr: cfg.serve, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	s.ctx = ctx
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	fmt.Printf("%sห้องเรียน/ห้องสอบ%s  http://localhost%s\n", bold, reset, normalizeAddr(cfg.serve))
	fmt.Printf("%sเอกสารจาก %s, ไฟล์ที่อัปโหลดเก็บที่ %s%s\n\n", dim, cfg.documentsDir(), uploadsDir, reset)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func normalizeAddr(addr string) string {
	if addr == "" {
		return ":80"
	}
	if addr[0] != ':' {
		return "/" + addr
	}
	return addr
}

// ---------------------------------------------------------------- jobs

type jobState string

const (
	jobRunning jobState = "running"
	jobDone    jobState = "done"
	jobFailed  jobState = "error"
)

// jobSnapshot is the whole of what a client can observe about a run. Progress
// arrives as replacements rather than as an append-only log: pass 1 emits
// thousands of updates and only the newest one is worth rendering.
type jobSnapshot struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	State   jobState        `json:"state"`
	Stage   string          `json:"stage"`
	Done    int             `json:"done"`
	Total   int             `json:"total"`
	Note    string          `json:"note"`
	Error   string          `json:"error,omitempty"`
	PrepKey string          `json:"prep_key,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

type webJob struct {
	mu   sync.Mutex
	snap jobSnapshot
	subs map[chan jobSnapshot]struct{}
}

func (s *webServer) newJob(kind string) *webJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	job := &webJob{
		snap: jobSnapshot{ID: "j" + strconv.Itoa(s.seq), Kind: kind, State: jobRunning, Stage: "เริ่ม"},
		subs: map[chan jobSnapshot]struct{}{},
	}
	s.jobs[job.snap.ID] = job
	return job
}

func (s *webServer) job(id string) *webJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

func (j *webJob) update(fn func(*jobSnapshot)) {
	j.mu.Lock()
	fn(&j.snap)
	snap := j.snap
	for ch := range j.subs {
		publishLatest(ch, snap)
	}
	j.mu.Unlock()
}

// publishLatest keeps one snapshot per subscriber. A slow browser must not be
// able to stall the pipeline, and an old progress line has no value once a
// newer one exists.
func publishLatest(ch chan jobSnapshot, snap jobSnapshot) {
	for {
		select {
		case ch <- snap:
			return
		default:
			select {
			case <-ch:
			default:
				return
			}
		}
	}
}

func (j *webJob) snapshot() jobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snap
}

func (j *webJob) subscribe() (<-chan jobSnapshot, func()) {
	ch := make(chan jobSnapshot, 1)
	j.mu.Lock()
	j.subs[ch] = struct{}{}
	publishLatest(ch, j.snap)
	j.mu.Unlock()
	return ch, func() {
		j.mu.Lock()
		delete(j.subs, ch)
		j.mu.Unlock()
	}
}

// progress is the sink handed to the pipeline for this job.
func (j *webJob) progress() examgen.Progress {
	return safeProgress(func(stage string, done, total int, note string) {
		j.update(func(snap *jobSnapshot) {
			snap.Stage = stage
			snap.Done = done
			snap.Total = total
			snap.Note = note
		})
	})
}

func (j *webJob) fail(err error) {
	j.update(func(snap *jobSnapshot) {
		snap.State = jobFailed
		snap.Error = err.Error()
	})
}

func (j *webJob) finish(fn func(*jobSnapshot)) {
	j.update(func(snap *jobSnapshot) {
		snap.State = jobDone
		snap.Stage = "เสร็จ"
		snap.Note = ""
		fn(snap)
	})
}

// ---------------------------------------------------------------- transport

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

func (s *webServer) handleJob(w http.ResponseWriter, r *http.Request) {
	job := s.job(r.PathValue("id"))
	if job == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("ไม่พบงาน %q", r.PathValue("id")))
		return
	}
	writeJSON(w, http.StatusOK, job.snapshot())
}

// handleJobEvents streams progress. Pass 1 runs for minutes and the browser
// has to show that something is happening rather than a spinner with no
// information behind it.
func (s *webServer) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	job := s.job(r.PathValue("id"))
	if job == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("ไม่พบงาน %q", r.PathValue("id")))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, cancel := job.subscribe()
	defer cancel()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case snap := <-events:
			// The result payload can be large; the stream carries state only and
			// the client fetches the finished job once.
			snap.Result = nil
			b, err := json.Marshal(snap)
			if err != nil {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
			if snap.State != jobRunning {
				return
			}
		}
	}
}
