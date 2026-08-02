package pdfx

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"protoexam/examgen"
	"protoexam/llm"
)

// ocrPrompt is close to the one the typhoon-ocr model card specifies. The model
// is task-specific: it is not a general vision model and drifts badly if asked
// anything else.
const ocrPrompt = `Extract all text from this image.

Return clean Markdown only. No explanation, no preamble, no code fence around
the whole answer. Include everything on the page in reading order. Render
tables as HTML and equations as LaTeX. Do not translate. Do not summarise. Do
not correct what the page says.`

// Rasteriser turns page N of a PDF into a PNG. Split out because it is the one
// piece with an external dependency and the one most likely to be swapped.
type Rasteriser interface {
	Render(pdfPath string, page int, outDir string) (string, error)
	Pages(pdfPath string) (int, error)
}

// ExtractOCR rasterises each page and reads it with a vision model.
//
// Slow on purpose: this is the escape hatch for documents whose text layer is
// unusable, which on Thai textbooks may be most of them.
func ExtractOCR(ctx context.Context, c *llm.Client, model, path string, from, to int, progress func(done, total int)) ([]examgen.Page, error) {
	ras, err := NewPoppler()
	if err != nil {
		return nil, err
	}

	total, err := ras.Pages(path)
	if err != nil {
		return nil, err
	}
	first, last := pageRange(from, to, total)

	tmp, err := os.MkdirTemp("", "protoexam-ocr-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	var pages []examgen.Page
	count := last - first + 1
	for i := first; i <= last; i++ {
		if progress != nil {
			progress(i-first, count)
		}
		if err := ctx.Err(); err != nil {
			return pages, err
		}

		png, err := ras.Render(path, i, tmp)
		if err != nil {
			return nil, fmt.Errorf("rasterise page %d: %w", i, err)
		}
		raw, err := os.ReadFile(png)
		if err != nil {
			return nil, err
		}

		msgs := []llm.Message{{
			Role:    "user",
			Content: ocrPrompt,
			Images:  []string{base64.StdEncoding.EncodeToString(raw)},
		}}
		// The model card is explicit that temperature must stay at or below 0.1
		// and that repetition penalty matters; higher values make it hallucinate
		// document structure.
		opt := &llm.Options{NumCtx: 8192, Temperature: 0.1, TopP: 0.6, RepeatPenalty: 1.1}

		text, err := c.Chat(ctx, model, msgs, opt)
		if err != nil {
			return nil, fmt.Errorf("ocr page %d: %w", i, err)
		}
		pages = append(pages, examgen.Page{Number: i, Text: strings.TrimSpace(text)})
		_ = os.Remove(png)
	}
	if progress != nil {
		progress(count, count)
	}
	return pages, nil
}

// Poppler shells out to pdftoppm. There is no pure-Go PDF rasteriser worth
// depending on; the cgo options bind MuPDF, which is AGPL and therefore a
// problem for the production server even though it would be fine here.
type Poppler struct {
	toppm string
	info  string
}

func NewPoppler() (*Poppler, error) {
	toppm, err := findTool("pdftoppm")
	if err != nil {
		return nil, err
	}
	info, _ := findTool("pdfinfo")
	return &Poppler{toppm: toppm, info: info}, nil
}

// Pages counts pages via pdfinfo, falling back to a generous guess when
// pdfinfo is missing so a partial poppler install still works with --pages.
func (p *Poppler) Pages(pdfPath string) (int, error) {
	if p.info == "" {
		return 0, fmt.Errorf("pdfinfo not found; pass an explicit --pages range")
	}
	out, err := exec.Command(p.info, pdfPath).Output()
	if err != nil {
		return 0, fmt.Errorf("pdfinfo: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "Pages:") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pages:")))
		if err != nil {
			return 0, err
		}
		return n, nil
	}
	return 0, fmt.Errorf("pdfinfo did not report a page count")
}

// Render writes one page as a PNG and returns its path.
//
// 200 dpi is the compromise: below ~150 Thai tone marks start merging into the
// characters above them, above 200 the image costs more tokens than it is worth.
func (p *Poppler) Render(pdfPath string, page int, outDir string) (string, error) {
	prefix := filepath.Join(outDir, fmt.Sprintf("page-%04d", page))
	cmd := exec.Command(p.toppm,
		"-png",
		"-r", "200",
		"-f", strconv.Itoa(page),
		"-l", strconv.Itoa(page),
		"-singlefile",
		pdfPath,
		prefix,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("pdftoppm: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return prefix + ".png", nil
}
