package extract

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"protoexam/examgen"
)

//go:embed docling_helper.py
var doclingHelper string

var ErrDoclingUnavailable = errors.New("Docling runtime is unavailable")

type DoclingOptions struct {
	Python      string
	PDF         string
	OutputDir   string
	From        int
	To          int
	OCREngine   string
	OCRLanguage string
	OCRMode     string
	FormulaMode string
	OCRFullPage bool
	Progress    ProgressFunc
	Runner      DoclingRunner
}

type DoclingResult struct {
	Pages             []examgen.Page
	Prepared          PreparedBundle
	ResolvedOCREngine string
	ResolvedOCRLang   []string
	ResolvedOCRMode   string
	ResolvedFormula   string
}

// DoclingRunner keeps subprocess behavior injectable so tests need neither
// Python nor model downloads.
type DoclingRunner interface {
	Run(ctx context.Context, executable string, args ...string) (stdout, stderr []byte, err error)
}

type ExecDoclingRunner struct{}

func (ExecDoclingRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

type doclingPayload struct {
	ResolvedOCREngine string         `json:"resolved_ocr_engine"`
	ResolvedOCRLang   []string       `json:"resolved_ocr_lang"`
	ResolvedOCRMode   string         `json:"resolved_ocr_mode"`
	ResolvedFormula   string         `json:"resolved_formula_mode"`
	Pages             []doclingPage  `json:"pages"`
	Assets            []doclingAsset `json:"assets"`
	Warnings          []string       `json:"warnings"`
}

type doclingPage struct {
	Number       int    `json:"number"`
	Markdown     string `json:"markdown"`
	PlainText    string `json:"plain_text"`
	MarkdownPath string `json:"markdown_path"`
}

type doclingAsset struct {
	Page       int        `json:"page"`
	Path       string     `json:"path"`
	Kind       string     `json:"kind"`
	MIMEType   string     `json:"mime"`
	SizeBytes  int64      `json:"size"`
	BBox       [4]float64 `json:"bbox"`
	Confidence float64    `json:"confidence"`
}

// ExtractDocling runs one local standard-pipeline conversion. Docling owns
// OCR/layout/table/image work and writes the Markdown/figure files directly;
// Go only turns its JSON envelope into pages and a validated common manifest.
func ExtractDocling(ctx context.Context, opts DoclingOptions) (DoclingResult, error) {
	python, err := ResolveDoclingPython(opts.Python)
	if err != nil {
		return DoclingResult{}, err
	}
	if strings.TrimSpace(opts.PDF) == "" {
		return DoclingResult{}, fmt.Errorf("Docling PDF path is empty")
	}
	if strings.TrimSpace(opts.OutputDir) == "" {
		return DoclingResult{}, fmt.Errorf("Docling output directory is empty")
	}
	if opts.To > 0 && opts.From > opts.To {
		return DoclingResult{}, fmt.Errorf("Docling page range is reversed: %d-%d", opts.From, opts.To)
	}
	if opts.Runner == nil {
		opts.Runner = ExecDoclingRunner{}
	}
	engine := strings.TrimSpace(opts.OCREngine)
	if engine == "" {
		engine = "auto"
	}
	language := strings.TrimSpace(opts.OCRLanguage)
	if language == "" {
		language = "auto"
	}
	if opts.Progress != nil {
		opts.Progress("extract/docling", 0, 0, "layout + OCR + figures")
	}
	args := []string{
		"-c", doclingHelper,
		"--pdf", opts.PDF,
		"--output-dir", opts.OutputDir,
		"--from-page", strconv.Itoa(opts.From),
		"--to-page", strconv.Itoa(opts.To),
		"--ocr-engine", engine,
		"--ocr-lang", language,
		"--ocr-mode", defaultMode(opts.OCRMode, "auto"),
		"--formula-mode", defaultMode(opts.FormulaMode, "auto"),
	}
	if opts.OCRFullPage {
		args = append(args, "--ocr-full-page")
	}
	type runResult struct {
		stdout []byte
		stderr []byte
		err    error
	}
	started := time.Now()
	done := make(chan runResult, 1)
	go func() {
		stdout, stderr, runErr := opts.Runner.Run(ctx, python, args...)
		done <- runResult{stdout: stdout, stderr: stderr, err: runErr}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var run runResult
	for {
		select {
		case run = <-done:
			goto finished
		case <-ticker.C:
			if opts.Progress != nil {
				opts.Progress("extract/docling", 0, selectedPageCount(opts.From, opts.To), fmt.Sprintf("layout + OCR + figures (%s)", time.Since(started).Round(time.Second)))
			}
		case <-ctx.Done():
			return DoclingResult{}, ctx.Err()
		}
	}

finished:
	stdout, stderr, err := run.stdout, run.stderr, run.err
	if err != nil {
		if ctx.Err() != nil {
			return DoclingResult{}, ctx.Err()
		}
		detail := strings.TrimSpace(string(stderr))
		if len(detail) > 2000 {
			detail = detail[len(detail)-2000:]
		}
		if detail == "" {
			detail = err.Error()
		}
		return DoclingResult{}, fmt.Errorf("Docling extraction failed: %s", detail)
	}

	var payload doclingPayload
	if err := json.Unmarshal(stdout, &payload); err != nil {
		return DoclingResult{}, fmt.Errorf("Docling helper returned invalid JSON: %w", err)
	}
	result := DoclingResult{
		Pages:             make([]examgen.Page, 0, len(payload.Pages)),
		ResolvedOCREngine: payload.ResolvedOCREngine,
		ResolvedOCRLang:   payload.ResolvedOCRLang,
		ResolvedOCRMode:   payload.ResolvedOCRMode,
		ResolvedFormula:   payload.ResolvedFormula,
		Prepared: PreparedBundle{
			DocumentMarkdownPath: "document.md",
			DocumentJSONPath:     "docling.json",
			Pages:                make([]PreparedBundlePage, 0, len(payload.Pages)),
			Assets:               make([]BundleAsset, 0, len(payload.Assets)),
			Warnings:             append([]string(nil), payload.Warnings...),
		},
	}
	for _, raw := range payload.Assets {
		kind := raw.Kind
		if kind == "" {
			kind = "figure"
		}
		result.Prepared.Assets = append(result.Prepared.Assets, BundleAsset{
			Page:       raw.Page,
			Path:       filepath.ToSlash(raw.Path),
			Kind:       kind,
			Label:      kind,
			Confidence: raw.Confidence,
			BBox:       raw.BBox,
			MIMEType:   raw.MIMEType,
			Available:  true,
			SizeBytes:  raw.SizeBytes,
		})
	}
	for _, raw := range payload.Pages {
		if raw.Number < 1 {
			return DoclingResult{}, fmt.Errorf("Docling returned invalid page number %d", raw.Number)
		}
		text := strings.TrimSpace(raw.PlainText)
		if strings.TrimSpace(text) == "" {
			text = MarkdownToPlainText(stripMarkdownImages(raw.Markdown))
		}
		result.Pages = append(result.Pages, examgen.Page{Number: raw.Number, Text: text})
		preparedPage := PreparedBundlePage{
			Number:       raw.Number,
			MarkdownPath: filepath.ToSlash(raw.MarkdownPath),
			Assets:       make([]BundleAsset, 0),
		}
		for i := range result.Prepared.Assets {
			asset := result.Prepared.Assets[i]
			if asset.Page == 0 && markdownReferencesAsset(raw.Markdown, asset.Path) {
				asset.Page = raw.Number
				result.Prepared.Assets[i].Page = raw.Number
			}
			if asset.Page == raw.Number {
				preparedPage.Assets = append(preparedPage.Assets, asset)
			}
		}
		result.Prepared.Pages = append(result.Prepared.Pages, preparedPage)
	}
	if opts.Progress != nil {
		opts.Progress("extract/docling", len(result.Pages), len(result.Pages), fmt.Sprintf("%s; %d figures", payload.ResolvedOCREngine, len(result.Prepared.Assets)))
	}
	return result, nil
}

func defaultMode(value, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	return value
}

func selectedPageCount(from, to int) int {
	if to <= 0 {
		return 0
	}
	if from < 1 {
		from = 1
	}
	if to < from {
		return 0
	}
	return to - from + 1
}

// ResolveDoclingPython finds the project-local runtime without mutating the
// user's global Python. An explicit flag or DOCLING_PYTHON always wins.
func ResolveDoclingPython(explicit string) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(os.Getenv("DOCLING_PYTHON")); value != "" {
		return value, nil
	}
	var starts []string
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(executable))
	}
	seen := make(map[string]struct{})
	for _, start := range starts {
		for dir := start; ; dir = filepath.Dir(dir) {
			clean := filepath.Clean(dir)
			if _, ok := seen[clean]; !ok {
				seen[clean] = struct{}{}
				candidates := []string{
					filepath.Join(clean, ".scratch", "docling-venv", "Scripts", "python.exe"),
					filepath.Join(clean, ".scratch", "docling-venv", "bin", "python"),
				}
				for _, candidate := range candidates {
					if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
						return candidate, nil
					}
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", fmt.Errorf("%w; run setup-docling.ps1, set DOCLING_PYTHON, or pass --docling-python", ErrDoclingUnavailable)
}

var markdownImage = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)

func stripMarkdownImages(markdown string) string {
	return strings.TrimSpace(markdownImage.ReplaceAllString(markdown, ""))
}

func markdownReferencesAsset(markdown, assetPath string) bool {
	path := filepath.ToSlash(assetPath)
	return strings.Contains(filepath.ToSlash(markdown), path) || strings.Contains(filepath.ToSlash(markdown), "../"+path)
}
