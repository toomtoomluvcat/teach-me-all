package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"protoexam/examgen"
	"protoexam/llm"
)

// uploadsDir is where a PDF supplied through the browser lands. It sits under
// the scratch tree because an uploaded file is run input, not a checked-in
// sample.
const uploadsDir = ".scratch/uploads"

func (cfg config) documentsDir() string {
	if strings.TrimSpace(cfg.docsDir) == "" {
		return "samples"
	}
	return cfg.docsDir
}

// docRef is one selectable source document.
type docRef struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Source string `json:"source"`
}

// ---------------------------------------------------------------- bootstrap

type providerView struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Note     string `json:"note"`
	NeedsKey bool   `json:"needs_key"`
	CanList  bool   `json:"can_list"`
	Model    string `json:"model"`
}

func (s *webServer) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	providers := []providerView{
		{ID: "ollama", Label: "Ollama (เครื่องนี้)", Note: "ไม่ต้องใช้คีย์ ต้อง pull โมเดลไว้ก่อน", CanList: true, Model: "scb10x/typhoon2.5-qwen3-4b"},
		{ID: "deepseek", Label: "DeepSeek", Note: "ใช้ DEEPSEEK_API_KEY จาก .env ถ้าไม่กรอก", NeedsKey: true, Model: "deepseek-chat"},
		{ID: "gemini", Label: "Google Gemini", Note: "ใช้ GEMINI_API_KEY จาก .env ถ้าไม่กรอก", NeedsKey: true, Model: "gemini-2.5-flash"},
		{ID: "openai", Label: "OpenAI-compatible", Note: "ต้องกรอก base URL และชื่อโมเดลเอง", NeedsKey: true, Model: ""},
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"providers":      providers,
		"styles":         examStyles(),
		"documents":      s.documents(),
		"default_host":   s.base.host,
		"candidates":     s.base.setCandidates,
		"docs_dir":       s.base.documentsDir(),
		"difficulty_map": difficultyLabels(),
	})
}

func difficultyLabels() map[string]string {
	return map[string]string{
		"":       "ตามที่เนื้อหารองรับ",
		"easy":   "ง่าย",
		"medium": "ปานกลาง",
		"hard":   "ยาก",
	}
}

// ---------------------------------------------------------------- documents

func (s *webServer) documents() []docRef {
	docs := append(listPDFs(s.base.documentsDir(), "samples"), listPDFs(uploadsDir, "uploads")...)
	sort.SliceStable(docs, func(i, j int) bool {
		if docs[i].Source != docs[j].Source {
			return docs[i].Source < docs[j].Source
		}
		return docs[i].Name < docs[j].Name
	})
	return docs
}

func listPDFs(dir, source string) []docRef {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var docs []docRef
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		docs = append(docs, docRef{
			Path:   filepath.ToSlash(filepath.Join(dir, e.Name())),
			Name:   e.Name(),
			Size:   info.Size(),
			Source: source,
		})
	}
	return docs
}

// resolveDoc accepts only a path the server itself listed. The browser never
// gets to name an arbitrary file on disk.
func (s *webServer) resolveDoc(path string) (docRef, error) {
	want := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	for _, doc := range s.documents() {
		if doc.Path == want {
			return doc, nil
		}
	}
	return docRef{}, fmt.Errorf("ไม่พบเอกสาร %q ในรายการ", path)
}

func (s *webServer) handleDocuments(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"documents": s.documents()})
}

func (s *webServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("อัปโหลดไม่สำเร็จ: %w", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()

	name := filepath.Base(header.Filename)
	if !strings.EqualFold(filepath.Ext(name), ".pdf") {
		writeError(w, http.StatusBadRequest, fmt.Errorf("รับเฉพาะไฟล์ .pdf"))
		return
	}
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	dest := filepath.Join(uploadsDir, name)
	out, err := os.Create(dest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	written, copyErr := io.Copy(out, file)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("เขียนไฟล์ไม่สำเร็จ: %w", cmpErr(copyErr, closeErr)))
		return
	}
	writeJSON(w, http.StatusOK, docRef{
		Path:   filepath.ToSlash(dest),
		Name:   name,
		Size:   written,
		Source: "uploads",
	})
}

func cmpErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// ---------------------------------------------------------------- models

// handleModels lists what the chosen provider can actually run. Only Ollama
// can answer that locally; for hosted providers the model is typed in, which
// is the same contract the CLI has.
func (s *webServer) handleModels(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if provider != "ollama" {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "listable": false})
		return
	}
	host := r.URL.Query().Get("host")
	if strings.TrimSpace(host) == "" {
		host = s.base.host
	}
	names, err := llm.New(host).Tags(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	sort.Strings(names)
	writeJSON(w, http.StatusOK, map[string]any{"models": names, "listable": true})
}

// ---------------------------------------------------------------- prepare

type modelRequest struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	EmbedModel string `json:"embed_model"`
	Host       string `json:"host"`
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
}

type prepareRequest struct {
	modelRequest
	Doc   string `json:"doc"`
	Pages string `json:"pages"`
	Fresh bool   `json:"fresh"`
}

// runConfig turns one browser request into the same validated config the CLI
// builds from flags. Provider defaults are re-resolved from scratch rather
// than inherited, or a run switched from DeepSeek to Ollama would keep asking
// for a model only DeepSeek has.
func (s *webServer) runConfig(req modelRequest) (config, error) {
	cfg := s.base
	cfg.progress = nil
	cfg.provider = strings.TrimSpace(req.Provider)
	if cfg.provider == "" {
		cfg.provider = "ollama"
	}
	cfg.model = strings.TrimSpace(req.Model)
	cfg.embedModel = strings.TrimSpace(req.EmbedModel)
	cfg.baseURL = strings.TrimSpace(req.BaseURL)
	cfg.apiKey = strings.TrimSpace(req.APIKey)
	if h := strings.TrimSpace(req.Host); h != "" {
		cfg.host = h
	}
	if cfg.provider == "gemini" && cfg.apiKey != "" {
		cfg.geminiAPIKey = cfg.apiKey
	}

	if err := applyProviderPreset(&cfg); err != nil {
		return config{}, err
	}
	if cfg.apiKey == "" {
		cfg.apiKey = os.Getenv("LLM_API_KEY")
	}
	if cfg.geminiAPIKey == "" {
		cfg.geminiAPIKey = os.Getenv("GEMINI_API_KEY")
	}
	// An embed model is only ever set explicitly from the browser, so an empty
	// value means "use the provider default", never "disable ranking".
	if err := resolveProviderDefaults(&cfg, cfg.embedModel != ""); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func (s *webServer) handlePrepare(w http.ResponseWriter, r *http.Request) {
	var req prepareRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	doc, err := s.resolveDoc(req.Doc)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg, err := s.runConfig(req.modelRequest)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg.pdfPath = doc.Path
	cfg.pages = strings.TrimSpace(req.Pages)
	cfg.fresh = req.Fresh
	if _, _, err := parsePages(cfg.pages); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	job := s.newJob("prepare")
	cfg.progress = job.progress()
	go s.runPrepare(job, cfg, doc)
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *webServer) runPrepare(job *webJob, cfg config, doc docRef) {
	ctx := s.ctx
	prep, err := func() (*preparedDoc, error) {
		if cfg.provider == "ollama" {
			job.progress()("ตรวจโมเดล", 0, 0, cfg.model)
			if err := preflight(ctx, llm.New(cfg.host), cfg); err != nil {
				return nil, err
			}
		}
		from, to, err := parsePages(cfg.pages)
		if err != nil {
			return nil, err
		}
		job.progress()("อ่านเอกสาร", 0, 0, doc.Name)
		extracted, err := extractDocument(ctx, cfg, from, to)
		if err != nil {
			return nil, err
		}
		if err := checkExtraction(cfg, extracted.Pages); err != nil {
			return nil, err
		}
		chunks := examgen.ChunkPages(extracted.Pages, examgen.DefaultChunkOptions())

		runtime, err := newModelRuntime(cfg)
		if err != nil {
			return nil, err
		}
		deps := buildDependencies(cfg, runtime.client)
		job.progress()("สรุปทั้งเล่ม", 0, 0, "ขั้นนี้ช้าที่สุด")
		outline, withLessons, err := buildOutline(ctx, cfg, chunks, deps)
		if err != nil {
			return nil, err
		}
		return &preparedDoc{
			Key: prepKey(cfg), cfg: cfg, Doc: doc, Outline: outline, Chunks: withLessons,
			Provider: cfg.provider, Model: cfg.model, Embed: cfg.embedModel,
			Pages: len(extracted.Pages), Prepared: time.Now(),
		}, nil
	}()
	if err != nil {
		job.fail(err)
		return
	}

	s.mu.Lock()
	s.preps[prep.Key] = prep
	s.mu.Unlock()

	job.finish(func(snap *jobSnapshot) { snap.PrepKey = prep.Key })
}

// prepKey names a prepared document by the same identity the disk cache uses,
// so two sessions that ask for the same pages of the same PDF share one entry.
func prepKey(cfg config) string { return filepath.Base(scratchDir(cfg)) }

func (s *webServer) prep(key string) (*preparedDoc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prep, ok := s.preps[key]
	if !ok {
		return nil, fmt.Errorf("เอกสารนี้ยังไม่ได้เตรียมในเซสชันนี้ — กลับไปขั้นเลือกเอกสาร")
	}
	return prep, nil
}

// ---------------------------------------------------------------- outline

type lessonView struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Budget   int    `json:"budget"`
	Chunks   int    `json:"chunks"`
	Concepts int    `json:"concepts"`
	FromPage int    `json:"from_page"`
	ToPage   int    `json:"to_page"`
}

func (s *webServer) handlePrep(w http.ResponseWriter, r *http.Request) {
	prep, err := s.prep(r.PathValue("key"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	byID := examgen.ChunkByID(prep.Chunks)
	lessons := make([]lessonView, 0, len(prep.Outline.Lessons))
	for _, lesson := range prep.Outline.Lessons {
		lessons = append(lessons, lessonToView(lesson, byID))
	}
	atoms := 0
	if prep.Outline.EvidenceGraph != nil {
		atoms = len(prep.Outline.EvidenceGraph.Atoms)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key":      prep.Key,
		"course":   prep.Outline.CourseTitle,
		"document": prep.Doc,
		"pages":    prep.Pages,
		"chunks":   len(prep.Chunks),
		"atoms":    atoms,
		"provider": prep.Provider,
		"model":    prep.Model,
		"lessons":  lessons,
	})
}

func lessonToView(lesson examgen.Lesson, byID map[string]examgen.Chunk) lessonView {
	view := lessonView{
		ID: lesson.ID, Title: lesson.Title, Summary: lesson.Summary,
		Budget: lesson.QuestionBudget, Chunks: len(lesson.ChunkIDs), Concepts: len(lesson.ConceptIDs),
	}
	for _, id := range lesson.ChunkIDs {
		chunk, ok := byID[id]
		if !ok {
			continue
		}
		if view.FromPage == 0 || chunk.Page < view.FromPage {
			view.FromPage = chunk.Page
		}
		if chunk.Page > view.ToPage {
			view.ToPage = chunk.Page
		}
	}
	return view
}

// handleLesson is the reading view. It serves the lesson's own chunks in
// document order — the same text the writer will be given, so what a student
// studies and what the questions are generated from cannot drift apart.
func (s *webServer) handleLesson(w http.ResponseWriter, r *http.Request) {
	prep, err := s.prep(r.PathValue("key"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	lesson, ok := findLesson(prep.Outline.Lessons, r.PathValue("lesson"))
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("ไม่พบบทเรียน %q", r.PathValue("lesson")))
		return
	}
	byID := examgen.ChunkByID(prep.Chunks)

	type block struct {
		ChunkID string `json:"chunk_id"`
		Page    int    `json:"page"`
		Text    string `json:"text"`
	}
	blocks := make([]block, 0, len(lesson.ChunkIDs))
	for _, id := range lesson.ChunkIDs {
		chunk, ok := byID[id]
		if !ok {
			continue
		}
		blocks = append(blocks, block{ChunkID: chunk.ID, Page: chunk.Page, Text: chunk.Text})
	}
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].Page < blocks[j].Page })

	writeJSON(w, http.StatusOK, map[string]any{
		"lesson": lessonToView(lesson, byID),
		"blocks": blocks,
	})
}

func findLesson(lessons []examgen.Lesson, id string) (examgen.Lesson, bool) {
	for _, lesson := range lessons {
		if lesson.ID == id {
			return lesson, true
		}
	}
	return examgen.Lesson{}, false
}

// ---------------------------------------------------------------- generate

type generateRequest struct {
	PrepKey    string `json:"prep_key"`
	LessonID   string `json:"lesson_id"`
	Count      int    `json:"count"`
	Skill      string `json:"skill"`
	Difficulty string `json:"difficulty"`
	Candidates int    `json:"candidates"`
}

func (s *webServer) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req generateRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	prep, err := s.prep(req.PrepKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	lesson, ok := findLesson(prep.Outline.Lessons, req.LessonID)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("ไม่พบบทเรียน %q", req.LessonID))
		return
	}
	directive, forceCalc, err := examDirective(req.Skill, req.Difficulty, lesson.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	opt := examgen.DefaultExamOptions()
	opt.Budget = req.Count
	opt.GenerationDirective = directive
	opt.ForceCalc = forceCalc
	opt.ContractPreflight = prep.cfg.contractPreflight
	opt.StopOnFullSet = prep.cfg.stopOnFullSet
	opt.SetCandidates = req.Candidates
	if opt.SetCandidates <= 0 {
		opt.SetCandidates = prep.cfg.setCandidates
	}

	job := s.newJob("generate")
	cfg := prep.cfg
	cfg.progress = job.progress()
	go s.runGenerate(job, prep, cfg, lesson, opt, req)
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *webServer) runGenerate(job *webJob, prep *preparedDoc, cfg config, lesson examgen.Lesson, opt examgen.ExamOptions, req generateRequest) {
	runtime, err := newModelRuntime(cfg)
	if err != nil {
		job.fail(err)
		return
	}
	deps := buildDependencies(cfg, runtime.client)
	res, err := examgen.GenerateExam(s.ctx, prep.Outline, lesson, prep.Chunks, deps, opt)
	if err != nil {
		job.fail(err)
		return
	}
	view := buildExamView(prep, lesson, res, req)
	// writeRun keeps the browser runs comparable with the terminal ones; the
	// artifacts directory is the prototype's evidence trail.
	if err := writeRun(cfg, res); err != nil {
		fmt.Printf("%swarning: could not write run artifact: %v%s\n", yellow, err, reset)
	}
	payload, err := json.Marshal(view)
	if err != nil {
		job.fail(err)
		return
	}
	job.finish(func(snap *jobSnapshot) { snap.Result = payload })
}

type choiceView struct {
	Label                string `json:"label"`
	Content              string `json:"content"`
	IsCorrect            bool   `json:"is_correct"`
	DistractorExpression string `json:"distractor_expression,omitempty"`
	DistractorAtomID     string `json:"distractor_atom_id,omitempty"`
}

type gateView struct {
	Gate          string `json:"gate"`
	Pass          bool   `json:"pass"`
	Reason        string `json:"reason,omitempty"`
	Deterministic bool   `json:"deterministic"`
}

type questionView struct {
	Number              int                  `json:"number"`
	Stem                string               `json:"stem"`
	Choices             []choiceView         `json:"choices"`
	Explanation         string               `json:"explanation"`
	SourceQuote         string               `json:"source_quote"`
	Skill               string               `json:"skill"`
	Difficulty          string               `json:"difficulty"`
	RequiresCalculation bool                 `json:"requires_calculation"`
	Calculation         *examgen.Calculation `json:"calculation,omitempty"`
	FlawedExpression    string               `json:"flawed_expression,omitempty"`
	DecoyValues         []string             `json:"decoy_values,omitempty"`
	ReasoningSteps      []string             `json:"reasoning_steps,omitempty"`
	DistractorReasons   []string             `json:"distractor_reasons,omitempty"`
	Page                int                  `json:"page"`
	ChunkID             string               `json:"chunk_id,omitempty"`
	AtomID              string               `json:"atom_id,omitempty"`
	SlotID              string               `json:"slot_id,omitempty"`
	Operation           string               `json:"operation,omitempty"`
	Passed              bool                 `json:"passed"`
	Gates               []gateView           `json:"gates,omitempty"`
}

type examView struct {
	Course     string                 `json:"course"`
	Document   string                 `json:"document"`
	Lesson     lessonView             `json:"lesson"`
	Skill      string                 `json:"skill"`
	Difficulty string                 `json:"difficulty"`
	Requested  int                    `json:"requested"`
	Budget     int                    `json:"budget"`
	Ceiling    bool                   `json:"ceiling"`
	Model      string                 `json:"model"`
	Provider   string                 `json:"provider"`
	Axes       examgen.AxisTally      `json:"axes"`
	Quality    *examgen.QualityReport `json:"quality,omitempty"`
	Questions  []questionView         `json:"questions"`
	Rejected   []questionView         `json:"rejected"`
}

// buildExamView splits the run into the paper a student sits and the drafts a
// gate rejected. Both are kept: hiding the rejects would throw away the only
// evidence that the gates are doing anything.
func buildExamView(prep *preparedDoc, lesson examgen.Lesson, res *examgen.ExamResult, req generateRequest) examView {
	byID := examgen.ChunkByID(prep.Chunks)
	view := examView{
		Course:     prep.Outline.CourseTitle,
		Document:   prep.Doc.Name,
		Lesson:     lessonToView(lesson, byID),
		Skill:      req.Skill,
		Difficulty: req.Difficulty,
		Requested:  req.Count,
		Budget:     res.Budget,
		Ceiling:    res.Ceiling,
		Model:      prep.Model,
		Provider:   prep.Provider,
		Axes:       res.Axes,
		Quality:    res.Quality,
	}
	for _, q := range res.Questions {
		item := questionToView(q, byID)
		if item.Passed {
			item.Number = len(view.Questions) + 1
			view.Questions = append(view.Questions, item)
			continue
		}
		item.Number = len(view.Rejected) + 1
		view.Rejected = append(view.Rejected, item)
	}
	return view
}

var choiceLabels = []string{"ก", "ข", "ค", "ง", "จ", "ฉ"}

func questionToView(q examgen.Question, byID map[string]examgen.Chunk) questionView {
	item := questionView{
		Stem: q.Stem, Explanation: q.Explanation, SourceQuote: q.SourceQuote,
		Skill: q.Skill, Difficulty: q.Difficulty, RequiresCalculation: q.RequiresCalculation,
		Calculation: q.Calculation, FlawedExpression: q.FlawedExpression,
		DecoyValues: q.DecoyValues, ReasoningSteps: q.ReasoningSteps,
		DistractorReasons: q.DistractorReasons,
		ChunkID:           q.EvidenceChunkID, AtomID: q.EvidenceAtomID, SlotID: q.CoverageSlotID,
		Operation: q.Operation,
		Passed:    q.Report != nil && q.Report.Passed(),
	}
	if chunk, ok := byID[q.EvidenceChunkID]; ok {
		item.Page = chunk.Page
	}
	for i, choice := range q.Choices {
		label := strconv.Itoa(i + 1)
		if i < len(choiceLabels) {
			label = choiceLabels[i]
		}
		item.Choices = append(item.Choices, choiceView{
			Label: label, Content: choice.Content, IsCorrect: choice.IsCorrect,
			DistractorExpression: choice.DistractorExpression,
			DistractorAtomID:     choice.DistractorAtomID,
		})
	}
	if q.Report != nil {
		for _, res := range q.Report.Results {
			item.Gates = append(item.Gates, gateView{
				Gate: string(res.Gate), Pass: res.Pass, Reason: res.Reason, Deterministic: res.Deterministic,
			})
		}
	}
	return item
}
