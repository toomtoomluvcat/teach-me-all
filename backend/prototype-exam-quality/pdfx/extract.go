// Package pdfx gets text out of a PDF, by two very different routes.
//
// The text route is fast and free and may produce garbage on Thai documents.
// The OCR route is slow and correct-ish. Which one is right is an empirical
// question the prototype exists to answer, so both are here and the caller
// picks.
package pdfx

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"

	"protoexam/examgen"
)

// ExtractText pulls the embedded text layer out of a PDF. from and to are
// 1-based and inclusive; 0 means "no limit".
//
// This fails silently-ish on scanned PDFs — they have no text layer, so it
// returns empty pages rather than an error. That is why the caller prints a
// sample before doing anything expensive with the result.
func ExtractText(path string, from, to int) ([]examgen.Page, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	total := r.NumPage()
	first, last := pageRange(from, to, total)

	var pages []examgen.Page
	for i := first; i <= last; i++ {
		p := r.Page(i)
		if p.V.IsNull() || p.V.Key("Contents").Kind() == pdf.Null {
			continue
		}
		text, err := pageText(p)
		if err != nil {
			// One unreadable page should not kill a 400 page book.
			continue
		}
		pages = append(pages, examgen.Page{Number: i, Text: text})
	}
	return pages, nil
}

// pageText rebuilds a page's lines from its text runs.
//
// GetPlainText is not usable here: it emits every text run on its own line, so
// "DLD-01" arrives as three lines and a Thai word split across two runs arrives
// split. Row-based extraction groups runs by their Y position, which is as
// close to "a line of the document" as a PDF will give you.
func pageText(p pdf.Page) (string, error) {
	rows, err := p.GetTextByRow()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for _, row := range rows {
		texts := append([]pdf.Text(nil), row.Content...)
		sort.SliceStable(texts, func(a, b int) bool { return texts[a].X < texts[b].X })

		var line strings.Builder
		prevEnd := 0.0
		prevRune := rune(0)
		for i, t := range texts {
			if i > 0 && needsSpace(t.X-prevEnd, t.FontSize, prevRune, firstRune(t.S)) {
				line.WriteString(" ")
			}
			line.WriteString(t.S)
			prevEnd = t.X + t.W
			if r := lastRune(t.S); r != 0 {
				prevRune = r
			}
		}
		s := strings.TrimRight(repairThai(line.String()), " ")
		if s == "" || isMojibake(s) {
			continue
		}
		b.WriteString(s)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// needsSpace decides whether the gap between two text runs is a real space.
//
// PDF writers split a single word into several runs whenever the font, kerning
// or encoding changes, so a naive "any gap is a space" rule turns "Gate" into
// "G ate". Thai is stricter still: it does not use spaces between words at all,
// so a gap between two Thai characters has to be large before it means anything.
func needsSpace(gap, fontSize float64, prev, next rune) bool {
	if fontSize <= 0 {
		fontSize = 10
	}
	threshold := fontSize * 0.55
	if isThai(prev) && isThai(next) {
		threshold = fontSize * 1.2
	}
	return gap > threshold
}

func isThai(r rune) bool { return r >= 0x0E00 && r <= 0x0E7F }

// isThaiConsonant covers ก through ฮ.
func isThaiConsonant(r rune) bool { return r >= 0x0E01 && r <= 0x0E2E }

// isThaiCombining covers the marks that attach above or below a consonant.
// None of them can legitimately follow a space: they have no standalone form.
func isThaiCombining(r rune) bool {
	switch {
	case r == 0x0E31: // MAI HAN AKAT
		return true
	case r >= 0x0E34 && r <= 0x0E3A: // sara i .. phinthu
		return true
	case r >= 0x0E47 && r <= 0x0E4E: // maitaikhu, tone marks, thanthakhat, ...
		return true
	}
	return false
}

// repairThai undoes two things PDF text extraction does to Thai.
//
// First: a space before a combining mark. Marks have no standalone form, so a
// space in front of one is always an extraction artefact — delete it.
//
// Second, and the one that actually matters: SARA AM (ำ, U+0E33) is drawn as
// two glyphs, NIKHAHIT above and SARA AA beside. Many PDFs have no character
// map for the NIKHAHIT glyph, so it extracts as nothing and leaves a gap:
// "คำ" comes out as "ค" + space + "า". Since า is a dependent vowel and can
// never follow a space in correct Thai, seeing that pattern means the NIKHAHIT
// was dropped, and putting ำ back is the right repair.
//
// This is a heuristic. It is applied because without it the verbatim-quote gate
// fails on every Thai question — the model writes "คำ" and the source says
// "ค า". If a document exists where it guesses wrong, --extract=ocr is the
// answer; read .scratch/*/extracted.txt before trusting a run.
func repairThai(s string) string {
	r := []rune(s)
	out := make([]rune, 0, len(r))
	for i := 0; i < len(r); i++ {
		if r[i] != ' ' {
			out = append(out, r[i])
			continue
		}
		next := rune(0)
		if i+1 < len(r) {
			next = r[i+1]
		}
		prev := rune(0)
		if n := len(out); n > 0 {
			prev = out[n-1]
		}

		if isThaiCombining(next) {
			continue // drop the space, keep the mark
		}
		if next == 0x0E32 && isThaiConsonant(prev) {
			out = append(out, 0x0E33) // ำ
			i++                       // consume the า we just replaced
			continue
		}
		out = append(out, ' ')
	}
	return string(out)
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func lastRune(s string) rune {
	var last rune
	for _, r := range s {
		last = r
	}
	return last
}

// isMojibake drops lines that are mostly replacement characters. Diagrams and
// figures embed fonts with no usable character map, and what comes out is
// noise that costs tokens and confuses pass 1. Those pages are the case for
// --extract=ocr, not something to feed the model as if it were text.
func isMojibake(s string) bool {
	var bad, total int
	for _, r := range s {
		if r == ' ' {
			continue
		}
		total++
		if r == '�' {
			bad++
		}
	}
	return total > 0 && bad*100/total > 30
}

func pageRange(from, to, total int) (int, int) {
	first, last := 1, total
	if from > 0 {
		first = from
	}
	if to > 0 && to < total {
		last = to
	}
	if first < 1 {
		first = 1
	}
	if last > total {
		last = total
	}
	return first, last
}
