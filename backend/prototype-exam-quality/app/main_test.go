package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"protoexam/examgen"
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
