package extract

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type stubDoclingRunner struct {
	stdout []byte
	stderr []byte
	err    error
	args   []string
}

func (s *stubDoclingRunner) Run(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
	s.args = append([]string(nil), args...)
	return s.stdout, s.stderr, s.err
}

func TestExtractAutoReturnsStructuredDoclingResult(t *testing.T) {
	runner := &stubDoclingRunner{stdout: []byte(`{
  "resolved_ocr_engine":"easyocr",
  "resolved_ocr_lang":["th","en"],
  "pages":[{"number":2,"markdown":"# Lesson","plain_text":"Lesson text","markdown_path":"pages/page-0002.md"}],
  "assets":[{"page":2,"path":"assets/figure.png","kind":"figure","mime":"image/png","size":12}],
  "warnings":[]
}`)}
	result, err := ExtractAuto(context.Background(), AutoOptions{
		PDF: "lesson.pdf", OutputDir: "bundle", From: 2, To: 2,
		Python: "fake-python", OCRLanguage: "th,en", DoclingRunner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "docling/easyocr" || result.Prepared == nil || len(result.Prepared.Assets) != 1 {
		t.Fatalf("unexpected structured result: %+v", result)
	}
	if len(result.Pages) != 1 || result.Pages[0].Text != "Lesson text" {
		t.Fatalf("chunking pages did not use plain text: %+v", result.Pages)
	}
	if !slices.Contains(runner.args, "--ocr-lang") || !slices.Contains(runner.args, "th,en") {
		t.Fatalf("Docling args omitted Thai/English OCR: %v", runner.args)
	}
	if !slices.Contains(runner.args, "--ocr-mode") || !slices.Contains(runner.args, "--formula-mode") {
		t.Fatalf("Docling args omitted adaptive extraction modes: %v", runner.args)
	}
}

func TestExtractAutoWarnsWhenFiguresAreMissingFromMarkdown(t *testing.T) {
	runner := &stubDoclingRunner{stdout: []byte(`{
  "resolved_ocr_engine":"easyocr",
  "pages":[{"number":4,"markdown":"See ![diagram](assets/a.png) and ![chart](assets/b.png)","plain_text":"See the diagram and chart","markdown_path":"pages/page-0004.md"}],
  "assets":[{"page":4,"path":"assets/a.png","kind":"figure","mime":"image/png","size":12}],
  "warnings":[]
}`)}
	result, err := ExtractAuto(context.Background(), AutoOptions{
		PDF: "lesson.pdf", OutputDir: "bundle", From: 4, To: 4,
		Python: "fake-python", DoclingRunner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range result.Prepared.Warnings {
		if strings.Contains(warning, "page 4") && strings.Contains(warning, "figure-extraction failure") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a figure-extraction-failure warning for page 4, got %v", result.Prepared.Warnings)
	}
}

func TestExtractAutoWarnsOnRaggedTable(t *testing.T) {
	markdown := "| A | B | C |\n|---|---|---|\n| 1 | 2 |\n| 3 | 4 | 5 |"
	runner := &stubDoclingRunner{stdout: fmt.Appendf(nil, `{
  "resolved_ocr_engine":"easyocr",
  "pages":[{"number":6,"markdown":%q,"plain_text":"table text","markdown_path":"pages/page-0006.md"}],
  "assets":[],
  "warnings":[]
}`, markdown)}
	result, err := ExtractAuto(context.Background(), AutoOptions{
		PDF: "lesson.pdf", OutputDir: "bundle", From: 6, To: 6,
		Python: "fake-python", DoclingRunner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range result.Prepared.Warnings {
		if strings.Contains(warning, "page 6") && strings.Contains(warning, "malformed table extraction") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a malformed-table warning for page 6, got %v", result.Prepared.Warnings)
	}
}

func TestExtractAutoDoesNotWarnOnWellFormedTable(t *testing.T) {
	markdown := "| A | B | C |\n|---|---|---|\n| 1 | 2 | 3 |\n| 4 | 5 | 6 |"
	runner := &stubDoclingRunner{stdout: fmt.Appendf(nil, `{
  "resolved_ocr_engine":"easyocr",
  "pages":[{"number":7,"markdown":%q,"plain_text":"table text","markdown_path":"pages/page-0007.md"}],
  "assets":[],
  "warnings":[]
}`, markdown)}
	result, err := ExtractAuto(context.Background(), AutoOptions{
		PDF: "lesson.pdf", OutputDir: "bundle", From: 7, To: 7,
		Python: "fake-python", DoclingRunner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range result.Prepared.Warnings {
		if strings.Contains(warning, "malformed table") {
			t.Fatalf("well-formed table should not warn, got %v", result.Prepared.Warnings)
		}
	}
}

func TestExtractAutoSurfacesDoclingFailureWithoutFallback(t *testing.T) {
	runner := &stubDoclingRunner{stderr: []byte("runtime failed"), err: errors.New("exit 1")}
	_, err := ExtractAuto(context.Background(), AutoOptions{
		PDF: "lesson.pdf", OutputDir: "bundle", Python: "fake-python", DoclingRunner: runner,
	})
	if err == nil || !strings.Contains(err.Error(), "auto extraction requires Docling") {
		t.Fatalf("expected explicit Docling failure, got %v", err)
	}
}
