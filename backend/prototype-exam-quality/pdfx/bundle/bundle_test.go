package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"protoexam/examgen"
)

func TestMarkdownToPlainTextPreservesVisibleContent(t *testing.T) {
	input := `# Heading

![diagram alt](diagram.png)

| First | Second |
| --- | --- |
| cell one | cell two |

<table><tr><td>HTML one</td><td>HTML two</td></tr></table>

[link text](https://example.test)

$E = mc^2$`

	got := MarkdownToPlainText(input)
	for _, want := range []string{"Heading", "diagram alt", "First Second", "cell one cell two", "HTML one HTML two", "link text", "E = mc^2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("MarkdownToPlainText() missing %q in %q", want, got)
		}
	}
	for _, unwanted := range []string{"diagram.png", "https://example.test", "<table>", "| --- |"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("MarkdownToPlainText() retained %q in %q", unwanted, got)
		}
	}
}

func TestWriteBundleRejectsMissingDoclingBundle(t *testing.T) {
	_, err := WriteBundle(BundleOptions{OutputDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "prepared Docling bundle is required") {
		t.Fatalf("expected required Docling bundle error, got %v", err)
	}
}

func TestWriteBundlePreservesPreparedFigureAssets(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(source, []byte("source bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "extract")
	for path, body := range map[string][]byte{
		"document.md":        []byte("Figure ![lungs](assets/lungs.png)\n"),
		"docling.json":       []byte("{}\n"),
		"pages/page-0001.md": []byte("Figure ![lungs](../assets/lungs.png)\n"),
		"assets/lungs.png":   {0x89, 'P', 'N', 'G'},
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	asset := BundleAsset{Page: 1, Path: "assets/lungs.png", Kind: "figure", MIMEType: "image/png", Available: true, SizeBytes: 4}
	result, err := WriteBundle(BundleOptions{
		OutputDir:      root,
		SourcePDF:      source,
		RequestedMode:  "auto",
		ResolvedMode:   "docling/easyocr",
		ExtractionMode: "docling/easyocr",
		Pages:          []examgen.Page{{Number: 1, Text: "Figure lungs"}},
		Prepared: &PreparedBundle{
			DocumentMarkdownPath: "document.md",
			DocumentJSONPath:     "docling.json",
			Pages:                []PreparedBundlePage{{Number: 1, MarkdownPath: "pages/page-0001.md", Assets: []BundleAsset{asset}}},
			Assets:               []BundleAsset{asset},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Manifest.Assets) != 1 || result.Manifest.Assets[0].Path != "assets/lungs.png" {
		t.Fatalf("prepared figure missing from manifest: %+v", result.Manifest.Assets)
	}
	if result.Manifest.SourceSHA256 == "" || result.Manifest.ResolvedMode != "docling/easyocr" {
		t.Fatalf("manifest metadata incomplete: %+v", result.Manifest)
	}
	if _, err := os.Stat(filepath.Join(root, "document.txt")); err != nil {
		t.Fatalf("text bundle was not written: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted BundleManifest
	if err := json.Unmarshal(manifestBytes, &persisted); err != nil {
		t.Fatalf("manifest.json is not valid JSON: %v", err)
	}
}
