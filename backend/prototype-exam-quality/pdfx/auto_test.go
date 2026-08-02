package pdfx

import (
	"context"
	"errors"
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
