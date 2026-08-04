package pdfx

import (
	"context"
	"fmt"

	"protoexam/examgen"
)

// ProgressFunc reports extraction work without coupling pdfx to terminal UI.
// total is zero while the Docling subprocess cannot expose a bounded count.
type ProgressFunc func(stage string, done, total int, note string)

type AutoOptions struct {
	PDF           string
	OutputDir     string
	From          int
	To            int
	Python        string
	OCREngine     string
	OCRLanguage   string
	OCRMode       string
	FormulaMode   string
	OCRFullPage   bool
	Progress      ProgressFunc
	DoclingRunner DoclingRunner
}

type AutoResult struct {
	Pages    []examgen.Page
	Mode     string
	Prepared *PreparedBundle
}

// ExtractAuto is deliberately a single-engine policy. Docling owns layout,
// OCR, tables, and figure extraction; failure is surfaced instead of silently
// degrading the document to text-only output.
func ExtractAuto(ctx context.Context, opts AutoOptions) (AutoResult, error) {
	result, err := ExtractDocling(ctx, DoclingOptions{
		Python:      opts.Python,
		PDF:         opts.PDF,
		OutputDir:   opts.OutputDir,
		From:        opts.From,
		To:          opts.To,
		OCREngine:   opts.OCREngine,
		OCRLanguage: opts.OCRLanguage,
		OCRMode:     opts.OCRMode,
		FormulaMode: opts.FormulaMode,
		OCRFullPage: opts.OCRFullPage,
		Progress:    opts.Progress,
		Runner:      opts.DoclingRunner,
	})
	if err != nil {
		return AutoResult{}, fmt.Errorf("auto extraction requires Docling: %w", err)
	}
	mode := "docling"
	if result.ResolvedOCRMode != "" {
		mode += "/" + result.ResolvedOCRMode
	} else if result.ResolvedOCREngine != "" {
		mode += "/" + result.ResolvedOCREngine
	}
	return AutoResult{Pages: result.Pages, Mode: mode, Prepared: &result.Prepared}, nil
}
