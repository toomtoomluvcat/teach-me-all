package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"protoexam/examgen"
	"protoexam/pdfx"
)

func TestExtractionDirDefaultAndExplicit(t *testing.T) {
	cfg := config{pdfPath: "lesson.pdf", extract: "auto", pages: "2-4"}
	if got, want := extractionDir(cfg), filepath.Join(scratchDir(cfg), "extract"); got != want {
		t.Fatalf("default extraction directory = %q, want %q", got, want)
	}
	cfg.extractDir = filepath.Join("custom", "bundle")
	if got := extractionDir(cfg); got != cfg.extractDir {
		t.Fatalf("explicit extraction directory = %q, want exact %q", got, cfg.extractDir)
	}
}

func TestRenderExtractionOnlyDoesNotOfferUnreadInput(t *testing.T) {
	out := captureStdout(t, func() {
		renderExtraction("lesson.pdf", "docling/easyocr", []examgen.Page{{Number: 1, Text: "lesson"}}, nil, false)
	})
	if strings.Contains(out, "[enter]") || strings.Contains(out, "[q + enter]") {
		t.Fatalf("non-interactive extraction offered keyboard actions: %q", out)
	}

	out = captureStdout(t, func() {
		renderExtraction("lesson.pdf", "docling/easyocr", []examgen.Page{{Number: 1, Text: "lesson"}}, nil, true)
	})
	if !strings.Contains(out, "[enter]") || !strings.Contains(out, "[q + enter]") {
		t.Fatalf("interactive extraction omitted keyboard actions: %q", out)
	}
}

func TestCheckExtractionListsWeakPagesInsteadOfJustACount(t *testing.T) {
	cfg := config{pdfPath: "lesson.pdf"}
	pages := []examgen.Page{
		{Number: 1, Text: strings.Repeat("x", 100)},
		{Number: 2, Text: ""},
		{Number: 3, Text: strings.Repeat("x", 100)},
	}
	out := captureStdout(t, func() {
		if err := checkExtraction(cfg, pages); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "note:") || !strings.Contains(out, "page(s) 2") {
		t.Fatalf("expected a note naming the weak page, got %q", out)
	}
}

func TestCheckExtractionEscalatesToWarningPastHalfEmpty(t *testing.T) {
	cfg := config{pdfPath: "lesson.pdf"}
	pages := []examgen.Page{
		{Number: 1, Text: strings.Repeat("x", 100)},
		{Number: 2, Text: ""},
		{Number: 3, Text: ""},
	}
	out := captureStdout(t, func() {
		if err := checkExtraction(cfg, pages); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "warning:") || !strings.Contains(out, "page(s) 2, 3") {
		t.Fatalf("expected a warning naming both weak pages, got %q", out)
	}
}

func TestCheckExtractionRejectsAllEmpty(t *testing.T) {
	cfg := config{pdfPath: "lesson.pdf"}
	pages := []examgen.Page{{Number: 1, Text: ""}, {Number: 2, Text: ""}}
	if err := checkExtraction(cfg, pages); err == nil || !strings.Contains(err.Error(), "no text") {
		t.Fatalf("expected a no-text error, got %v", err)
	}
}

func TestFormatPageListCapsLongRuns(t *testing.T) {
	pages := make([]int, 20)
	for i := range pages {
		pages[i] = i + 1
	}
	got := formatPageList(pages)
	if !strings.Contains(got, "and 5 more") {
		t.Fatalf("expected the tail to be summarized, got %q", got)
	}
}

func TestPrintExtractionWarningsSurfacesFigureMismatch(t *testing.T) {
	prepared := &pdfx.PreparedBundle{Warnings: []string{
		"page 4: markdown references 2 image(s) but only 1 were extracted as assets — possible figure-extraction failure",
	}}
	out := captureStdout(t, func() { printExtractionWarnings(prepared) })
	if !strings.Contains(out, "page 4") || !strings.Contains(out, "figure-extraction failure") {
		t.Fatalf("expected the figure-mismatch warning to be printed, got %q", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out)
}
