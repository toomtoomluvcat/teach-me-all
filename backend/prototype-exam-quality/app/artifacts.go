package app

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"protoexam/examgen"
	"protoexam/pdfx"
)

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

type runQuestionArtifact struct {
	examgen.Question
	Passed bool                 `json:"passed"`
	Gates  []examgen.GateResult `json:"gates"`
}

type runArtifact struct {
	Lesson           string                    `json:"lesson"`
	Budget           int                       `json:"budget"`
	PassRate         float64                   `json:"pass_rate"`
	Ceiling          bool                      `json:"ceiling"`
	SetCandidates    int                       `json:"set_candidates,omitempty"`
	SelectedSetScore int                       `json:"selected_set_score,omitempty"`
	Quality          *examgen.QualityReport    `json:"quality,omitempty"`
	Contract         *examgen.CoverageContract `json:"contract,omitempty"`
	Questions        []runQuestionArtifact     `json:"questions"`
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
	out := runArtifact{
		Lesson:           res.Lesson.Title,
		Budget:           res.Budget,
		PassRate:         res.PassRate(),
		Ceiling:          res.Ceiling,
		SetCandidates:    res.SetCandidates,
		SelectedSetScore: res.SelectedSetScore,
		Quality:          res.Quality,
		Contract:         res.Contract,
	}

	for _, q := range res.Questions {
		r := runQuestionArtifact{Question: q}
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

func scratchDir(cfg config) string {
	h := sha1.Sum([]byte(strings.Join([]string{
		cfg.pdfPath, cfg.extract, cfg.pages, cfg.doclingOCRMode,
		cfg.doclingFormulaMode, cfg.doclingOCREngine, cfg.doclingOCRLang,
		strconv.FormatBool(cfg.doclingOCRFullPage),
	}, "|")))
	return filepath.Join(".scratch", hex.EncodeToString(h[:8]))
}

func extractionDir(cfg config) string {
	if cfg.extractDir != "" {
		return cfg.extractDir
	}
	return filepath.Join(scratchDir(cfg), "extract")
}

// cachedT memoises a slow step to disk. Extraction and pass 1 take minutes and
// rerunning them while iterating on a prompt is pure waste.
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
