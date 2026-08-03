// Command protoexam is a THROWAWAY prototype. See README.md.
//
// It answers one question: does a chunk-grounded, gate-verified pipeline
// produce multiple-choice questions a human can actually interpret?
//
// It is not a service, it has no database, and its terminal shell is not meant
// to survive. The examgen package is the part worth keeping.
package main

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"protoexam/examgen"
	"protoexam/llm"
	"protoexam/pdfx"
)

type config struct {
	provider           string
	pdfPath            string
	extract            string
	model              string
	embedModel         string
	extractDir         string
	doclingPython      string
	doclingOCREngine   string
	doclingOCRLang     string
	doclingOCRFullPage bool
	host               string
	geminiHost         string
	geminiAPIKey       string
	deepseekHost       string
	deepseekAPIKey     string
	geminiMinInterval  time.Duration
	forceCalc          bool
	scope              string
	pages              string
	budget             int
	fresh              bool
	extractOnly        bool
	calcTool           bool
	parallel           int
}

type extractionCache struct {
	Pages    []examgen.Page       `json:"pages"`
	Mode     string               `json:"mode"`
	Prepared *pdfx.PreparedBundle `json:"prepared,omitempty"`
}

func main() {
	_ = loadDotEnv()
	var cfg config
	flag.StringVar(&cfg.provider, "provider", "ollama", "model provider: ollama | gemini | deepseek")
	flag.StringVar(&cfg.pdfPath, "pdf", "", "path to the source PDF (required)")
	flag.StringVar(&cfg.extract, "extract", "auto", "extraction mode: auto | docling (both use Docling; auto is the default name)")
	flag.StringVar(&cfg.extractDir, "extract-dir", "", "directory for the extraction bundle; default .scratch/<hash>/extract")
	flag.StringVar(&cfg.doclingPython, "docling-python", "", "Python with Docling installed; auto-detected from .scratch/docling-venv")
	flag.StringVar(&cfg.doclingOCREngine, "docling-ocr-engine", "auto", "Docling OCR engine: auto | rapidocr | easyocr")
	flag.StringVar(&cfg.doclingOCRLang, "docling-ocr-lang", "th,en", "Docling OCR languages; defaults to Thai + English")
	flag.BoolVar(&cfg.doclingOCRFullPage, "docling-ocr-full-page", false, "force OCR over complete pages instead of only detected regions")
	flag.StringVar(&cfg.model, "model", "", "generation and judge model (provider default when empty)")
	// bge-m3, not nomic-embed-text. Measured on Thai question pairs,
	// nomic returns cosine 1.0000 for every pair in the same chapter whether
	// they are the same question or not — no threshold separates them, and
	// chunk ranking built on it is random. bge-m3 scores duplicates at 0.95+
	// and different questions at 0.60, a usable gap of 0.31.
	flag.StringVar(&cfg.embedModel, "embed-model", "", "embedding model, empty to use the provider default")
	flag.StringVar(&cfg.host, "host", "http://localhost:11434", "Ollama host")
	flag.StringVar(&cfg.geminiHost, "gemini-host", "https://generativelanguage.googleapis.com", "Gemini API host")
	flag.StringVar(&cfg.geminiAPIKey, "gemini-api-key", "", "Gemini API key (prefer GEMINI_API_KEY)")
	flag.DurationVar(&cfg.geminiMinInterval, "gemini-min-interval", 13*time.Second, "minimum delay between Gemini requests; 0 disables throttling")
	flag.StringVar(&cfg.deepseekHost, "deepseek-host", "https://api.deepseek.com", "DeepSeek API host")
	flag.StringVar(&cfg.deepseekAPIKey, "deepseek-api-key", "", "DeepSeek API key (prefer DEEPSEEK_API_KEY)")
	flag.BoolVar(&cfg.forceCalc, "force-calc", false, "generate calculation questions only")
	flag.StringVar(&cfg.scope, "scope", "", "free-text focus; chunks are ranked against this instead of the lesson title")
	flag.StringVar(&cfg.pages, "pages", "", "page range, e.g. 10-40")
	flag.IntVar(&cfg.budget, "budget", 0, "override the model's own question budget")
	flag.BoolVar(&cfg.fresh, "fresh", false, "ignore the cache")
	flag.BoolVar(&cfg.extractOnly, "extract-only", false, "stop after extraction; needs no models and no Ollama")
	flag.BoolVar(&cfg.calcTool, "calc-tool", true, "let the model use a calculator tool before writing calculation questions")
	flag.IntVar(&cfg.parallel, "parallel", 4, "model calls in flight at once; Ollama also needs OLLAMA_NUM_PARALLEL to match")
	flag.Parse()
	if cfg.geminiAPIKey == "" {
		cfg.geminiAPIKey = os.Getenv("GEMINI_API_KEY")
	}
	if cfg.deepseekAPIKey == "" {
		cfg.deepseekAPIKey = os.Getenv("DEEPSEEK_API_KEY")
	}
	if cfg.provider != "ollama" && cfg.provider != "gemini" && cfg.provider != "deepseek" {
		fmt.Fprintf(os.Stderr, "--provider must be ollama, gemini, or deepseek, got %q\n", cfg.provider)
		os.Exit(2)
	}
	if cfg.model == "" {
		switch cfg.provider {
		case "gemini":
			cfg.model = "gemini-2.5-flash"
		case "deepseek":
			cfg.model = "deepseek-chat"
		default:
			cfg.model = "scb10x/typhoon2.5-qwen3-4b"
		}
	}
	// An explicitly empty --embed-model still disables ranking. Only fill in a
	// provider default when the flag was not supplied at all.
	embedModelSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "embed-model" {
			embedModelSet = true
		}
	})
	if !embedModelSet {
		switch cfg.provider {
		case "gemini":
			cfg.embedModel = "gemini-embedding-001"
		case "deepseek":
			// DeepSeek exposes chat completions, not embeddings. Leave ranking
			// disabled unless a separate embedder is wired later.
			cfg.embedModel = ""
		default:
			cfg.embedModel = "bge-m3"
		}
	}

	if cfg.pdfPath == "" {
		fmt.Fprintln(os.Stderr, "--pdf is required")
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "\n%serror:%s %v\n", red, reset, err)
		os.Exit(1)
	}
}

// loadDotEnv loads the nearest .env from the working directory or one of its
// parents. It is intentionally tiny: this prototype only needs key=value
// pairs, and an already-exported environment variable always wins.
func loadDotEnv() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		path := filepath.Join(dir, ".env")
		data, err := os.ReadFile(path)
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				line = strings.TrimPrefix(line, "export ")
				at := strings.IndexByte(line, '=')
				if at <= 0 {
					continue
				}
				key := strings.TrimSpace(line[:at])
				if os.Getenv(key) != "" {
					continue
				}
				value := strings.TrimSpace(line[at+1:])
				if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
					if unquoted, unquoteErr := strconv.Unquote(value); unquoteErr == nil {
						value = unquoted
					}
				} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
					value = value[1 : len(value)-1]
				}
				_ = os.Setenv(key, value)
			}
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func run(ctx context.Context, cfg config) error {
	in := bufio.NewScanner(os.Stdin)
	var (
		client      *llm.Client
		modelClient llm.ModelClient
		stats       *llm.Stats
	)
	if cfg.provider == "ollama" {
		client = llm.New(cfg.host)
		modelClient = client
		stats = client.Stats
	} else {
		if cfg.provider == "gemini" {
			if !cfg.extractOnly && cfg.geminiAPIKey == "" {
				return fmt.Errorf("Gemini provider requires GEMINI_API_KEY or --gemini-api-key")
			}
			gemini := llm.NewGeminiAt(cfg.geminiHost, cfg.geminiAPIKey, nil)
			gemini.MinInterval = cfg.geminiMinInterval
			modelClient = gemini
			stats = gemini.Stats
		} else {
			if !cfg.extractOnly && cfg.deepseekAPIKey == "" {
				return fmt.Errorf("DeepSeek provider requires DEEPSEEK_API_KEY or --deepseek-api-key")
			}
			deepseek := llm.NewDeepSeekAt(cfg.deepseekHost, cfg.deepseekAPIKey, nil)
			modelClient = deepseek
			stats = deepseek.Stats
		}
	}
	defer reportStats(cfg, stats)

	// Fail here rather than twenty minutes into pass 1. Skipped for
	// --extract-only, whose whole point is to inspect extraction on a machine
	// with no models pulled yet.
	if cfg.provider == "ollama" && !cfg.extractOnly {
		if err := preflight(ctx, client, cfg); err != nil {
			return err
		}
	}

	// --- step 1: extract ----------------------------------------------------
	from, to, err := parsePages(cfg.pages)
	if err != nil {
		return err
	}

	renderProgress("extract", 0, 0, "checking cache")
	// v3 is Docling-only. Older caches may contain fallback text without the
	// structured Markdown/figure envelope and must never satisfy this path.
	extracted, err := cachedT(cfg, "pages-v3", func() (extractionCache, error) {
		switch cfg.extract {
		case "auto":
			result, err := pdfx.ExtractAuto(ctx, pdfx.AutoOptions{
				PDF: cfg.pdfPath, OutputDir: extractionDir(cfg), From: from, To: to,
				Python: cfg.doclingPython, OCREngine: cfg.doclingOCREngine,
				OCRLanguage: cfg.doclingOCRLang, OCRFullPage: cfg.doclingOCRFullPage,
				Progress: renderProgress,
			})
			return extractionCache{Pages: result.Pages, Mode: result.Mode, Prepared: result.Prepared}, err
		case "docling":
			result, err := pdfx.ExtractDocling(ctx, pdfx.DoclingOptions{
				Python: cfg.doclingPython, PDF: cfg.pdfPath, OutputDir: extractionDir(cfg),
				From: from, To: to, OCREngine: cfg.doclingOCREngine,
				OCRLanguage: cfg.doclingOCRLang, OCRFullPage: cfg.doclingOCRFullPage,
				Progress: renderProgress,
			})
			mode := "docling"
			if result.ResolvedOCREngine != "" {
				mode += "/" + result.ResolvedOCREngine
			}
			return extractionCache{Pages: result.Pages, Mode: mode, Prepared: &result.Prepared}, err
		default:
			return extractionCache{}, fmt.Errorf("--extract must be auto or docling, got %q", cfg.extract)
		}
	})
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	pages := extracted.Pages
	resolved := extracted.Mode
	if resolved == "" {
		resolved = cfg.extract
	}
	displayMode := resolved
	if cfg.extract == "auto" {
		displayMode = "auto -> " + resolved
	}
	renderProgress("extract", len(pages), len(pages), "ready: "+displayMode)
	chunks := examgen.ChunkPages(pages, examgen.DefaultChunkOptions())
	renderExtraction(cfg.pdfPath, displayMode, pages, chunks, !cfg.extractOnly)
	// A scanned PDF has pages but no characters on them. Counting pages is not
	// enough to notice — count what actually came out.
	verdict := checkExtraction(cfg, pages)
	if cfg.extractOnly {
		if verdict != nil {
			fmt.Printf("\n%s%v%s\n", red, verdict, reset)
		}
		if err := writeExtractionBundle(cfg, resolved, from, to, pages, extracted.Prepared); err != nil {
			return err
		}
		return dumpExtraction(cfg, pages)
	}
	if verdict != nil {
		return verdict
	}
	if !confirm(in) {
		return nil
	}

	// --- step 2: outline ----------------------------------------------------
	gen := llm.NewGenerator(modelClient, cfg.model)
	gen.UseCalcTool = cfg.calcTool

	deps := examgen.Deps{
		Gen:      gen,
		Judge:    llm.NewJudge(modelClient, cfg.model),
		Eval:     examgen.Arith{},
		Log:      examgen.Progress(safeProgress()),
		Parallel: cfg.parallel,
	}
	if cfg.provider == "gemini" || cfg.provider == "deepseek" {
		deps.TopicBatcher = llm.NewBatchedTopicGenerator(modelClient, cfg.model, cfg.parallel)
	}
	if cfg.embedModel != "" {
		deps.Embedder = llm.NewEmbedder(modelClient, cfg.embedModel)
	}

	type outlineCache struct {
		Outline *examgen.Outline
		Chunks  []examgen.Chunk
	}

	oc, err := cachedT(cfg, "outline-v2", func() (outlineCache, error) {
		clear()
		header("STEP 2 — reading the whole document (pass 1)")
		mapPlan := "one model call each"
		if cfg.provider == "gemini" || cfg.provider == "deepseek" {
			mapCalls := llm.PlannedTopicBatches(chunks)
			mapPlan = fmt.Sprintf("%d bounded map calls + 1 reduce call", mapCalls)
		}
		fmt.Printf("%sThis is the slow part. %d chunks, %s.%s\n\n", dim, len(chunks), mapPlan, reset)
		o, withLessons, err := examgen.BuildOutline(ctx, chunks, deps)
		fmt.Println()
		return outlineCache{Outline: o, Chunks: withLessons}, err
	})
	if err != nil {
		return err
	}
	outline, chunks := oc.Outline, oc.Chunks

	// --- step 3: pick a lesson, generate, review ----------------------------
	for {
		renderOutline(outline, chunks)
		line, ok := readLine(in)
		if !ok || line == "q" {
			return nil
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(outline.Lessons) {
			continue
		}
		lesson := outline.Lessons[n-1]

		opt := examgen.DefaultExamOptions()
		opt.ForceCalc = cfg.forceCalc
		opt.Scope = cfg.scope
		opt.Budget = cfg.budget

		clear()
		header("STEP 3 — generating and gating: " + lesson.Title)
		fmt.Println()
		res, err := examgen.GenerateExam(ctx, outline, lesson, chunks, deps, opt)
		fmt.Println()
		if err != nil {
			return err
		}
		if len(res.Questions) == 0 {
			fmt.Printf("%sNothing was generated from this lesson at all.%s\n", yellow, reset)
			fmt.Print("press enter ")
			readLine(in)
			continue
		}

		if err := review(in, res); err != nil {
			return err
		}
		if err := writeRun(cfg, res); err != nil {
			return err
		}
	}
}

func reportStats(cfg config, stats *llm.Stats) {
	if r := stats.Report(); r != "" {
		title := "where the time went"
		if cfg.provider == "gemini" {
			title = "Gemini API calls and timing (cumulative for this process)"
		} else if cfg.provider == "deepseek" {
			title = "DeepSeek API calls and timing (cumulative for this process)"
		}
		fmt.Printf("\n%s%s%s\n%s", bold, title, reset, r)
	}
}

// minRunesPerPage is the floor below which a page is assumed to be an image.
// Even a sparse slide carries more than this.
const minRunesPerPage = 40

// checkExtraction refuses to spend twenty minutes of model time on a document
// that has no text in it.
func checkExtraction(cfg config, pages []examgen.Page) error {
	total := 0
	empty := 0
	for _, p := range pages {
		n := examgen.RuneLen(strings.TrimSpace(p.Text))
		total += n
		if n < minRunesPerPage {
			empty++
		}
	}

	if total == 0 {
		return fmt.Errorf("Docling returned no text for any of the %d pages in %s — inspect the extraction bundle and OCR settings", len(pages), cfg.pdfPath)
	}
	if empty*2 > len(pages) {
		fmt.Printf("\n%swarning: %d of %d pages came out effectively empty; inspect those pages and adjust Docling OCR settings if needed.%s\n",
			yellow, empty, len(pages), reset)
	}
	return nil
}

// dumpExtraction writes the whole extracted text to a file so the full thing
// can be read in an editor, not just the two-page sample on screen. Extraction
// quality is the biggest unknown in this prototype and eyeballing 600 runes of
// page one is not enough to judge it.
func dumpExtraction(cfg config, pages []examgen.Page) error {
	dir := scratchDir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "extracted.txt")

	var b strings.Builder
	for _, p := range pages {
		fmt.Fprintf(&b, "\n===== page %d =====\n%s\n", p.Number, p.Text)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("\n%sfull text written to %s%s\n", dim, path, reset)
	return nil
}

func writeExtractionBundle(cfg config, mode string, from, to int, pages []examgen.Page, prepared *pdfx.PreparedBundle) error {
	result, err := pdfx.WriteBundle(pdfx.BundleOptions{
		OutputDir:      extractionDir(cfg),
		SourcePDF:      cfg.pdfPath,
		RequestedMode:  cfg.extract,
		ResolvedMode:   mode,
		ExtractionMode: mode, // compatibility alias for older consumers
		From:           from,
		To:             to,
		Pages:          pages,
		Prepared:       prepared,
		Progress:       renderProgress,
	})
	if err != nil {
		return fmt.Errorf("write extraction bundle: %w", err)
	}
	for _, warning := range result.Warnings {
		fmt.Printf("\n%swarning: %s%s\n", yellow, warning, reset)
	}
	fmt.Printf("\n%sextraction bundle written to %s%s\n", dim, extractionDir(cfg), reset)
	return nil
}

// preflight checks the models are pulled before anything slow starts.
func preflight(ctx context.Context, c *llm.Client, cfg config) error {
	want := []string{cfg.model}
	if cfg.embedModel != "" {
		want = append(want, cfg.embedModel)
	}
	var missing []string
	for _, m := range want {
		ok, err := c.Installed(ctx, m)
		if err != nil {
			return err
		}
		if !ok {
			missing = append(missing, m)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString("these models are not pulled yet:\n")
	for _, m := range missing {
		fmt.Fprintf(&b, "  ollama pull %s\n", m)
	}
	return fmt.Errorf("%s", b.String())
}

// review is the read-and-judge loop. This is where the human half of the
// verdict happens.
func review(in *bufio.Scanner, res *examgen.ExamResult) error {
	idx := 0
	for {
		renderQuestion(res, idx)
		line, ok := readLine(in)
		if !ok {
			return nil
		}
		switch line {
		case "q":
			return nil
		case "n", "":
			if idx < len(res.Questions)-1 {
				idx++
			}
		case "p":
			if idx > 0 {
				idx--
			}
		case "f":
			for i := idx + 1; i < len(res.Questions); i++ {
				if res.Questions[i].Report != nil && !res.Questions[i].Report.Passed() {
					idx = i
					break
				}
			}
		case "s":
			renderSummary(res)
			if l, ok := readLine(in); !ok || l == "q" {
				return nil
			}
		}
	}
}

// writeRun drops the whole run next to the prototype so runs can be compared
// after the fact and pasted next to NotebookLM's output.
func writeRun(cfg config, res *examgen.ExamResult) error {
	dir := scratchDir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("run-%s.json", sanitise(res.Lesson.Title))
	path := filepath.Join(dir, name)

	type reported struct {
		examgen.Question
		Passed bool                 `json:"passed"`
		Gates  []examgen.GateResult `json:"gates"`
	}
	out := struct {
		Lesson    string     `json:"lesson"`
		Budget    int        `json:"budget"`
		PassRate  float64    `json:"pass_rate"`
		Ceiling   bool       `json:"ceiling"`
		Questions []reported `json:"questions"`
	}{Lesson: res.Lesson.Title, Budget: res.Budget, PassRate: res.PassRate(), Ceiling: res.Ceiling}

	for _, q := range res.Questions {
		r := reported{Question: q}
		if q.Report != nil {
			r.Passed = q.Report.Passed()
			r.Gates = q.Report.Results
		}
		out.Questions = append(out.Questions, r)
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("\n%swrote %s%s\n", dim, path, reset)
	return nil
}

// --- plumbing ---------------------------------------------------------------

func readLine(in *bufio.Scanner) (string, bool) {
	if !in.Scan() {
		return "", false
	}
	return strings.TrimSpace(in.Text()), true
}

func confirm(in *bufio.Scanner) bool {
	line, ok := readLine(in)
	return ok && line != "q"
}

func parsePages(s string) (int, int, error) {
	if strings.TrimSpace(s) == "" {
		return 0, 0, nil
	}
	parts := strings.SplitN(s, "-", 2)
	from, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("--pages %q: %w", s, err)
	}
	if len(parts) == 1 {
		return from, from, nil
	}
	to, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("--pages %q: %w", s, err)
	}
	return from, to, nil
}

func scratchDir(cfg config) string {
	h := sha1.Sum([]byte(cfg.pdfPath + "|" + cfg.extract + "|" + cfg.pages))
	return filepath.Join(".scratch", hex.EncodeToString(h[:8]))
}

func extractionDir(cfg config) string {
	if cfg.extractDir != "" {
		return cfg.extractDir
	}
	return filepath.Join(scratchDir(cfg), "extract")
}

// cached memoises a slow step to disk. Extraction and pass 1 take minutes and
// rerunning them while iterating on a prompt is pure waste.
func cached[T any](cfg config, name string, produce func() (T, error)) (T, error) {
	return cachedT(cfg, name, produce)
}

func cachedT[T any](cfg config, name string, produce func() (T, error)) (T, error) {
	var zero T
	dir := scratchDir(cfg)
	path := filepath.Join(dir, name+".json")

	if !cfg.fresh {
		if b, err := os.ReadFile(path); err == nil {
			var v T
			if json.Unmarshal(b, &v) == nil {
				return v, nil
			}
		}
	}

	v, err := produce()
	if err != nil {
		return zero, err
	}
	if err := os.MkdirAll(dir, 0o755); err == nil {
		if b, err := json.Marshal(v); err == nil {
			_ = os.WriteFile(path, b, 0o644)
		}
	}
	return v, nil
}

func sanitise(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "lesson"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}
