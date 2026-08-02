package pdfx

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"protoexam/examgen"
)

// BundleOptions describes the durable files written for one extraction.
// Prepared is set by a document engine such as Docling when it has already
// written Markdown and figure assets. The bundle writer then only validates
// those files and writes the common text/manifest envelope.
type BundleOptions struct {
	OutputDir      string
	SourcePDF      string
	RequestedMode  string
	ResolvedMode   string
	ExtractionMode string
	From           int
	To             int
	Pages          []examgen.Page
	Prepared       *PreparedBundle
	Progress       ProgressFunc
}

// PreparedBundle points at Markdown and figure files already written below
// BundleOptions.OutputDir by a structured document extractor.
type PreparedBundle struct {
	DocumentMarkdownPath string               `json:"document_markdown_path"`
	DocumentJSONPath     string               `json:"document_json_path,omitempty"`
	Pages                []PreparedBundlePage `json:"pages"`
	Assets               []BundleAsset        `json:"assets"`
	Warnings             []string             `json:"warnings,omitempty"`
}

type PreparedBundlePage struct {
	Number       int           `json:"number"`
	MarkdownPath string        `json:"markdown_path"`
	Assets       []BundleAsset `json:"assets"`
}

// BundleResult reports the manifest and non-fatal rendering warnings.
type BundleResult struct {
	Manifest BundleManifest
	Warnings []string
}

type BundleManifest struct {
	SchemaVersion  int             `json:"schema_version"`
	SourcePDF      string          `json:"source_pdf"`
	SourceSHA256   string          `json:"source_sha256,omitempty"`
	RequestedMode  string          `json:"requested_mode"`
	ResolvedMode   string          `json:"resolved_mode"`
	ExtractionMode string          `json:"extraction_mode"`
	PageRange      BundlePageRange `json:"page_range"`
	Pages          []BundlePage    `json:"pages"`
	Assets         []BundleAsset   `json:"assets"`
	Warnings       []string        `json:"warnings,omitempty"`
}

type BundlePageRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type BundlePage struct {
	Number       int           `json:"number"`
	MarkdownPath string        `json:"markdown_path"`
	TextPath     string        `json:"text_path"`
	Assets       []BundleAsset `json:"assets"`
}

type BundleAsset struct {
	Page       int        `json:"page"`
	Path       string     `json:"path"`
	Kind       string     `json:"kind"`
	Label      string     `json:"label,omitempty"`
	Confidence float64    `json:"confidence,omitempty"`
	BBox       [4]float64 `json:"bbox,omitempty"`
	MIMEType   string     `json:"mime_type"`
	Available  bool       `json:"available"`
	SizeBytes  int64      `json:"size_bytes,omitempty"`
}

// WriteBundle validates Docling's prepared Markdown/figure graph and writes the
// common plain-text and manifest envelope around it.
func WriteBundle(opts BundleOptions) (BundleResult, error) {
	if strings.TrimSpace(opts.OutputDir) == "" {
		return BundleResult{}, fmt.Errorf("bundle output directory is empty")
	}
	if opts.Prepared == nil {
		return BundleResult{}, fmt.Errorf("prepared Docling bundle is required")
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return BundleResult{}, fmt.Errorf("create bundle directory %q: %w", opts.OutputDir, err)
	}
	pages := append([]examgen.Page(nil), opts.Pages...)
	seen := make(map[int]struct{}, len(pages))
	for _, page := range pages {
		if page.Number < 1 {
			return BundleResult{}, fmt.Errorf("page number must be positive, got %d", page.Number)
		}
		if _, ok := seen[page.Number]; ok {
			return BundleResult{}, fmt.Errorf("duplicate page number %d", page.Number)
		}
		seen[page.Number] = struct{}{}
	}

	manifest := BundleManifest{
		SchemaVersion:  1,
		SourcePDF:      opts.SourcePDF,
		RequestedMode:  opts.RequestedMode,
		ResolvedMode:   opts.ResolvedMode,
		ExtractionMode: opts.ExtractionMode,
		PageRange:      BundlePageRange{From: opts.From, To: opts.To},
		Pages:          make([]BundlePage, 0, len(pages)),
		Assets:         make([]BundleAsset, 0),
	}
	preparedPages := make(map[int]PreparedBundlePage)
	for _, page := range opts.Prepared.Pages {
		if _, exists := preparedPages[page.Number]; exists {
			return BundleResult{}, fmt.Errorf("duplicate prepared page number %d", page.Number)
		}
		preparedPages[page.Number] = page
	}
	manifest.Assets = append(manifest.Assets, opts.Prepared.Assets...)

	var documentText strings.Builder
	for i, page := range pages {
		base := fmt.Sprintf("pages/page-%04d", page.Number)
		prepared, ok := preparedPages[page.Number]
		if !ok {
			return BundleResult{}, fmt.Errorf("prepared bundle omitted page %d", page.Number)
		}
		if err := requireArtifact(opts.OutputDir, prepared.MarkdownPath); err != nil {
			return BundleResult{}, fmt.Errorf("prepared Markdown for page %d: %w", page.Number, err)
		}
		textPath := base + ".txt"
		if err := writeArtifact(opts.OutputDir, textPath, []byte(ensureTrailingNewline(page.Text))); err != nil {
			return BundleResult{}, fmt.Errorf("write page %d text: %w", page.Number, err)
		}

		fmt.Fprintf(&documentText, "Page %d\n\n%s\n\n", page.Number, strings.TrimSpace(page.Text))
		manifest.Pages = append(manifest.Pages, BundlePage{
			Number:       page.Number,
			MarkdownPath: prepared.MarkdownPath,
			TextPath:     textPath,
			Assets:       append([]BundleAsset(nil), prepared.Assets...),
		})
		if opts.Progress != nil {
			opts.Progress("extract/store", i+1, len(pages), fmt.Sprintf("page %d text", page.Number))
		}
	}

	if err := writeArtifact(opts.OutputDir, "document.txt", []byte(documentText.String())); err != nil {
		return BundleResult{}, fmt.Errorf("write document text: %w", err)
	}

	warnings := append([]string(nil), opts.Prepared.Warnings...)
	if opts.SourcePDF != "" {
		if sum, err := sha256File(opts.SourcePDF); err != nil {
			warnings = append(warnings, "source SHA-256 unavailable: "+err.Error())
		} else {
			manifest.SourceSHA256 = sum
		}
	}
	documentPath := opts.Prepared.DocumentMarkdownPath
	if documentPath == "" {
		documentPath = "document.md"
	}
	if err := requireArtifact(opts.OutputDir, documentPath); err != nil {
		return BundleResult{}, fmt.Errorf("prepared document Markdown: %w", err)
	}
	for _, asset := range manifest.Assets {
		if err := requireArtifact(opts.OutputDir, asset.Path); err != nil {
			return BundleResult{}, fmt.Errorf("prepared asset %q: %w", asset.Path, err)
		}
	}
	manifest.Warnings = warnings

	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return BundleResult{}, fmt.Errorf("encode bundle manifest: %w", err)
	}
	if err := writeArtifact(opts.OutputDir, "manifest.json", append(b, '\n')); err != nil {
		return BundleResult{}, fmt.Errorf("write bundle manifest: %w", err)
	}
	return BundleResult{Manifest: manifest, Warnings: warnings}, nil
}

func requireArtifact(root, relative string) error {
	path, err := safeArtifactPath(root, relative)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	return nil
}

func ensureTrailingNewline(text string) string {
	return strings.TrimRight(text, "\r\n") + "\n"
}

func safeArtifactPath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("artifact path must be relative: %q", relative)
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes bundle directory: %q", relative)
	}
	return filepath.Join(root, clean), nil
}

func writeArtifact(root, relative string, data []byte) error {
	path, err := safeArtifactPath(root, relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

var markdownEquation = regexp.MustCompile(`(?s)\$\$.*?\$\$|\$[^$\n]+?\$|\\\[.*?\\\]|\\\(.*?\\\)`)

// MarkdownToPlainText converts extracted Markdown to readable text without
// throwing away words hidden in links, images, tables, HTML, or equations.
func MarkdownToPlainText(markdown string) string {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	markdown = strings.ReplaceAll(markdown, "\r", "\n")

	var equations []string
	markdown = markdownEquation.ReplaceAllStringFunc(markdown, func(match string) string {
		equations = append(equations, equationContents(match))
		return fmt.Sprintf("\x00EQ%d\x00", len(equations)-1)
	})
	markdown = replaceMarkdownLinks(markdown)
	markdown = stripHTMLTags(markdown)

	lines := strings.Split(markdown, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		line = stripBlockMarker(line)
		if isTableSeparator(line) || markdownOnly(line) {
			lines[i] = ""
			continue
		}
		line = strings.ReplaceAll(line, "|", " ")
		line = stripInlineMarkdown(line)
		lines[i] = strings.Join(strings.Fields(line), " ")
	}

	var out []string
	for _, line := range lines {
		if line != "" {
			out = append(out, line)
		}
	}
	text := strings.Join(out, "\n")
	for i, equation := range equations {
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00EQ%d\x00", i), equation)
	}
	return strings.TrimSpace(text)
}

func equationContents(s string) string {
	switch {
	case strings.HasPrefix(s, "$$") && strings.HasSuffix(s, "$$"):
		return strings.TrimSpace(s[2 : len(s)-2])
	case strings.HasPrefix(s, "$") && strings.HasSuffix(s, "$"):
		return strings.TrimSpace(s[1 : len(s)-1])
	case strings.HasPrefix(s, "\\[") && strings.HasSuffix(s, "\\]"):
		return strings.TrimSpace(s[2 : len(s)-2])
	case strings.HasPrefix(s, "\\(") && strings.HasSuffix(s, "\\)"):
		return strings.TrimSpace(s[2 : len(s)-2])
	default:
		return s
	}
}

func replaceMarkdownLinks(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		start := i
		image := false
		if s[i] == '!' && i+1 < len(s) && s[i+1] == '[' {
			image = true
			start = i + 1
		} else if s[i] != '[' {
			out.WriteByte(s[i])
			i++
			continue
		}
		close := matchingDelimiter(s, start, '[', ']')
		if close < 0 {
			out.WriteByte(s[i])
			i++
			continue
		}
		end := close + 1
		valid := false
		if end < len(s) && s[end] == '(' {
			if destinationEnd := matchingDelimiter(s, end, '(', ')'); destinationEnd >= 0 {
				end = destinationEnd + 1
				valid = true
			}
		} else if end < len(s) && s[end] == '[' {
			if referenceEnd := matchingDelimiter(s, end, '[', ']'); referenceEnd >= 0 {
				end = referenceEnd + 1
				valid = true
			}
		}
		if !valid {
			out.WriteByte(s[i])
			i++
			continue
		}
		if image {
			out.WriteString(s[start+1 : close])
		} else {
			out.WriteString(s[start+1 : close])
		}
		i = end
	}
	return out.String()
}

func matchingDelimiter(s string, start int, open, close byte) int {
	depth := 0
	for i := start; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		switch s[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func stripHTMLTags(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '<' {
			out.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i+1:], '>')
		if end < 0 {
			out.WriteByte(s[i])
			i++
			continue
		}
		end += i + 1
		tag := s[i : end+1]
		if isHTMLTag(tag) {
			out.WriteByte(' ')
			i = end + 1
			continue
		}
		out.WriteString(tag)
		i = end + 1
	}
	return out.String()
}

func isHTMLTag(tag string) bool {
	trimmed := strings.TrimSpace(tag)
	if len(trimmed) < 3 || trimmed[0] != '<' || trimmed[len(trimmed)-1] != '>' || strings.Contains(trimmed, "://") {
		return false
	}
	i := 1
	if trimmed[i] == '/' || trimmed[i] == '!' || trimmed[i] == '?' {
		i++
	}
	return i < len(trimmed)-1 && unicode.IsLetter(rune(trimmed[i]))
}

func stripBlockMarker(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, ">") {
		line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
	}
	for n := 0; n < len(line) && n < 6 && line[n] == '#'; n++ {
		if n == len(line)-1 || (line[n+1] != '#') {
			if n+1 < len(line) && unicode.IsSpace(rune(line[n+1])) {
				return strings.TrimSpace(line[n+2:])
			}
		}
	}
	if len(line) >= 2 && ((line[0] == '-' || line[0] == '*' || line[0] == '+') && unicode.IsSpace(rune(line[1]))) {
		return strings.TrimSpace(line[2:])
	}
	for i := 0; i < len(line) && line[i] >= '0' && line[i] <= '9'; i++ {
		if i+1 < len(line) && line[i+1] == '.' && i+2 < len(line) && unicode.IsSpace(rune(line[i+2])) {
			return strings.TrimSpace(line[i+3:])
		}
	}
	return line
}

func isTableSeparator(line string) bool {
	if !strings.Contains(line, "|") {
		return false
	}
	for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
		cell = strings.TrimSpace(strings.Trim(cell, ":"))
		if cell == "" || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func markdownOnly(line string) bool {
	if line == "" {
		return true
	}
	for _, r := range line {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("\x00", r) {
			return false
		}
	}
	return true
}

func stripInlineMarkdown(line string) string {
	var out strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) && strings.ContainsRune("\\`*_{}[]()#+-.!|>", rune(line[i+1])) {
			out.WriteByte(line[i+1])
			i++
			continue
		}
		switch line[i] {
		case '*', '_', '`', '~', '[', ']':
			continue
		default:
			out.WriteByte(line[i])
		}
	}
	return out.String()
}
