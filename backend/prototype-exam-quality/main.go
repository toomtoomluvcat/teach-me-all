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

	"protoexam/examgen"
	"protoexam/llm"
	"protoexam/pdfx"
)

type config struct {
	pdfPath     string
	extract     string
	model       string
	embedModel  string
	ocrModel    string
	host        string
	forceCalc   bool
	scope       string
	pages       string
	budget      int
	fresh       bool
	extractOnly bool
	repair      bool
	calcTool    bool
}

func main() {
	var cfg config
	flag.StringVar(&cfg.pdfPath, "pdf", "", "path to the source PDF (required)")
	flag.StringVar(&cfg.extract, "extract", "auto", "extraction mode: auto | text | poppler | ocr")
	flag.StringVar(&cfg.model, "model", "scb10x/typhoon2.5-qwen3-4b", "generation and judge model")
	flag.StringVar(&cfg.embedModel, "embed-model", "nomic-embed-text", "embedding model, empty to disable ranking")
	flag.StringVar(&cfg.ocrModel, "ocr-model", "scb10x/typhoon-ocr1.5-3b", "vision model used by --extract=ocr")
	flag.StringVar(&cfg.host, "host", "http://localhost:11434", "Ollama host")
	flag.BoolVar(&cfg.forceCalc, "force-calc", false, "generate calculation questions only")
	flag.StringVar(&cfg.scope, "scope", "", "free-text focus; chunks are ranked against this instead of the lesson title")
	flag.StringVar(&cfg.pages, "pages", "", "page range, e.g. 10-40")
	flag.IntVar(&cfg.budget, "budget", 0, "override the model's own question budget")
	flag.BoolVar(&cfg.fresh, "fresh", false, "ignore the cache")
	flag.BoolVar(&cfg.extractOnly, "extract-only", false, "stop after extraction; needs no models and no Ollama")
	flag.BoolVar(&cfg.repair, "repair", false, "send questions rejected by a deterministic gate back to the model once (measured worthless on 4B)")
	flag.BoolVar(&cfg.calcTool, "calc-tool", true, "let the model use a calculator tool before writing calculation questions")
	flag.Parse()

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

func run(ctx context.Context, cfg config) error {
	in := bufio.NewScanner(os.Stdin)
	client := llm.New(cfg.host)

	// Fail here rather than twenty minutes into pass 1. Skipped for
	// --extract-only, whose whole point is to inspect extraction on a machine
	// with no models pulled yet.
	if !cfg.extractOnly || cfg.extract == "ocr" {
		if err := preflight(ctx, client, cfg); err != nil {
			return err
		}
	}

	// --- step 1: extract ----------------------------------------------------
	from, to, err := parsePages(cfg.pages)
	if err != nil {
		return err
	}

	chosen := cfg.extract
	pages, err := cached(cfg, "pages", func() ([]examgen.Page, error) {
		switch cfg.extract {
		case "auto":
			p, mode, err := pdfx.ExtractAuto(cfg.pdfPath, from, to)
			chosen = "auto -> " + mode
			return p, err
		case "text":
			return pdfx.ExtractText(cfg.pdfPath, from, to)
		case "poppler":
			return pdfx.ExtractPoppler(cfg.pdfPath, from, to)
		case "ocr":
			renderProgress("ocr", 0, 0, "loading "+cfg.ocrModel)
			return pdfx.ExtractOCR(ctx, client, cfg.ocrModel, cfg.pdfPath, from, to, func(done, total int) {
				renderProgress("ocr", done, total, "rasterise + read")
			})
		default:
			return nil, fmt.Errorf("--extract must be auto, text, poppler or ocr, got %q", cfg.extract)
		}
	})
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	chunks := examgen.ChunkPages(pages, examgen.DefaultChunkOptions())
	renderExtraction(cfg.pdfPath, chosen, pages, chunks)
	// A scanned PDF has pages but no characters on them. Counting pages is not
	// enough to notice — count what actually came out.
	verdict := checkExtraction(cfg, pages)
	if cfg.extractOnly {
		if verdict != nil {
			fmt.Printf("\n%s%v%s\n", red, verdict, reset)
		}
		return dumpExtraction(cfg, pages)
	}
	if verdict != nil {
		return verdict
	}
	if !confirm(in) {
		return nil
	}

	// The OCR model and the generation model cannot both sit in 6 GB of VRAM,
	// so the one we are done with is evicted before the next is loaded.
	if cfg.extract == "ocr" {
		_ = client.Unload(ctx, cfg.ocrModel)
	}

	// --- step 2: outline ----------------------------------------------------
	gen := llm.NewGenerator(client, cfg.model)
	gen.UseCalcTool = cfg.calcTool

	deps := examgen.Deps{
		Gen:   gen,
		Judge: llm.NewJudge(client, cfg.model),
		Eval:  examgen.Arith{},
		Log:   examgen.Progress(renderProgress),
	}
	if cfg.embedModel != "" {
		deps.Embedder = llm.NewEmbedder(client, cfg.embedModel)
	}

	type outlineCache struct {
		Outline *examgen.Outline
		Chunks  []examgen.Chunk
	}

	oc, err := cachedT(cfg, "outline", func() (outlineCache, error) {
		clear()
		header("STEP 2 — reading the whole document (pass 1)")
		fmt.Printf("%sThis is the slow part. %d chunks, one model call each.%s\n\n", dim, len(chunks), reset)
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
		opt.Repair = cfg.repair

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
		if cfg.extract == "ocr" {
			return fmt.Errorf("OCR returned nothing for any of the %d pages — check the model and the rasterised images", len(pages))
		}
		return fmt.Errorf("%s has %d pages and no text layer at all — it is a scan.\nrerun with --extract=ocr", cfg.pdfPath, len(pages))
	}
	if empty*2 > len(pages) {
		fmt.Printf("\n%swarning: %d of %d pages came out effectively empty. If those pages matter, use --extract=ocr.%s\n",
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

// preflight checks the models are pulled before anything slow starts.
func preflight(ctx context.Context, c *llm.Client, cfg config) error {
	want := []string{cfg.model}
	if cfg.embedModel != "" {
		want = append(want, cfg.embedModel)
	}
	if cfg.extract == "ocr" {
		want = append(want, cfg.ocrModel)
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
