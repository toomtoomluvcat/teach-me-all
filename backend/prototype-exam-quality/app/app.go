package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"protoexam/examgen"
	"protoexam/llm"
	"protoexam/pdfx"
)

type modelRuntime struct {
	ollama *llm.Client
	client llm.ModelClient
	stats  *llm.Stats
}

type outlineCache struct {
	Outline *examgen.Outline
	Chunks  []examgen.Chunk
}

func run(ctx context.Context, cfg config) error {
	in := bufio.NewScanner(os.Stdin)
	runtime, err := newModelRuntime(cfg)
	if err != nil {
		return err
	}
	defer reportStats(cfg, runtime.stats)

	// Fail here rather than twenty minutes into pass 1. Skipped for
	// --extract-only, whose whole point is to inspect extraction on a machine
	// with no models pulled yet.
	if cfg.provider == "ollama" && !cfg.extractOnly {
		if err := preflight(ctx, runtime.ollama, cfg); err != nil {
			return err
		}
	}

	from, to, err := parsePages(cfg.pages)
	if err != nil {
		return err
	}

	renderProgress("extract", 0, 0, "checking cache")
	extracted, err := extractDocument(ctx, cfg, from, to)
	if err != nil {
		return err
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
	chunks := examgen.ChunkPages(pages, examgen.DefaultChunkOptions())
	renderProgress("extract", len(pages), len(pages), "ready: "+displayMode)
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

	deps := buildDependencies(cfg, runtime.client)
	outline, chunks, err := buildOutline(ctx, cfg, chunks, deps)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.benchmark) != "" {
		return runBenchmark(ctx, cfg, outline, chunks, deps)
	}
	return interactiveGeneration(ctx, in, cfg, outline, chunks, deps)
}

func newModelRuntime(cfg config) (modelRuntime, error) {
	if cfg.provider == "ollama" {
		client := llm.New(cfg.host)
		return modelRuntime{ollama: client, client: client, stats: client.Stats}, nil
	}
	if cfg.provider == "gemini" {
		if !cfg.extractOnly && cfg.geminiAPIKey == "" {
			return modelRuntime{}, fmt.Errorf("Gemini provider requires GEMINI_API_KEY or --gemini-api-key")
		}
		client := llm.NewGeminiAt(cfg.geminiHost, cfg.geminiAPIKey, nil)
		client.MinInterval = cfg.geminiMinInterval
		return modelRuntime{client: client, stats: client.Stats}, nil
	}
	// No key check here: a local OpenAI-compatible server usually wants none,
	// and a hosted one reports a missing key far more precisely than a guess
	// made before the first request.
	client := llm.NewOpenAICompatibleAt(cfg.baseURL, cfg.apiKey, nil)
	return modelRuntime{client: client, stats: client.Stats}, nil
}

func buildDependencies(cfg config, modelClient llm.ModelClient) examgen.Deps {
	gen := llm.NewGenerator(modelClient, cfg.model)
	gen.UseCalcTool = cfg.calcTool

	deps := examgen.Deps{
		Gen:           gen,
		Eval:          examgen.Arith{},
		Quality:       llm.NewQualityGrader(modelClient, cfg.model),
		Log:           examgen.Progress(safeProgress()),
		Parallel:      cfg.parallel,
		KeepAllTopics: !cfg.filterTopics,
	}
	if hostedProvider(cfg.provider) {
		deps.TopicBatcher = llm.NewBatchedTopicGenerator(modelClient, cfg.model, cfg.parallel)
	}
	if cfg.embedModel != "" {
		deps.Embedder = llm.NewEmbedder(modelClient, cfg.embedModel)
	}
	return deps
}

// hostedProvider reports whether the provider has a context window big enough
// to map every chunk in one request. The native Ollama client keeps its
// measured one-call-per-chunk path instead.
func hostedProvider(provider string) bool {
	return provider == "gemini" || provider == "openai"
}

func extractDocument(ctx context.Context, cfg config, from, to int) (extractionCache, error) {
	// v3 is Docling-only. Older caches may contain fallback text without the
	// structured Markdown/figure envelope and must never satisfy this path.
	extracted, err := cachedT(cfg, "pages-v3", func() (extractionCache, error) {
		switch cfg.extract {
		case "auto":
			result, err := pdfx.ExtractAuto(ctx, pdfx.AutoOptions{
				PDF: cfg.pdfPath, OutputDir: extractionDir(cfg), From: from, To: to,
				Python: cfg.doclingPython, OCREngine: cfg.doclingOCREngine,
				OCRLanguage: cfg.doclingOCRLang, OCRMode: cfg.doclingOCRMode,
				FormulaMode: cfg.doclingFormulaMode, OCRFullPage: cfg.doclingOCRFullPage,
				Progress: renderProgress,
			})
			return extractionCache{Pages: result.Pages, Mode: result.Mode, Prepared: result.Prepared}, err
		case "docling":
			result, err := pdfx.ExtractDocling(ctx, pdfx.DoclingOptions{
				Python: cfg.doclingPython, PDF: cfg.pdfPath, OutputDir: extractionDir(cfg),
				From: from, To: to, OCREngine: cfg.doclingOCREngine,
				OCRLanguage: cfg.doclingOCRLang, OCRMode: cfg.doclingOCRMode,
				FormulaMode: cfg.doclingFormulaMode, OCRFullPage: cfg.doclingOCRFullPage,
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
		return extractionCache{}, fmt.Errorf("extract: %w", err)
	}
	return extracted, nil
}

func buildOutline(ctx context.Context, cfg config, chunks []examgen.Chunk, deps examgen.Deps) (*examgen.Outline, []examgen.Chunk, error) {
	// v3: pass 1 now classifies every topic as content, apparatus or page
	// furniture. A v2 cache was built before that existed and still holds answer
	// keys and assessment rubrics as concepts, with nothing left in the code able
	// to tell which ones they are. Reusing it would silently serve lessons the
	// current pipeline would never have produced.
	//
	// v4: evidence compilation is no longer optional, so every cached outline
	// carries atoms. A v3 cache has none and set generation cannot run on it.
	oc, err := cachedT(cfg, "outline-v4", func() (outlineCache, error) {
		clear()
		header("STEP 2 — reading the whole document (pass 1)")
		mapPlan := "one model call each"
		if hostedProvider(cfg.provider) {
			mapPlan = fmt.Sprintf("%d bounded map calls + 1 reduce call", llm.PlannedTopicBatches(chunks))
		}
		fmt.Printf("%sThis is the slow part. %d chunks, %s.%s\n\n", dim, len(chunks), mapPlan, reset)
		o, withLessons, err := examgen.BuildOutline(ctx, chunks, deps)
		fmt.Println()
		return outlineCache{Outline: o, Chunks: withLessons}, err
	})
	if err != nil {
		return nil, nil, err
	}
	return oc.Outline, oc.Chunks, nil
}

func interactiveGeneration(ctx context.Context, in *bufio.Scanner, cfg config, outline *examgen.Outline, chunks []examgen.Chunk, deps examgen.Deps) error {
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
		opt.Budget = cfg.budget
		opt.SetCandidates = cfg.setCandidates
		opt.ContractPreflight = cfg.contractPreflight
		opt.StopOnFullSet = cfg.stopOnFullSet

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

const minRunesPerPage = 40

// checkExtraction refuses to spend twenty minutes of model time on a
// document that has no text in it.
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
