package pdfx

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unicode"

	"protoexam/examgen"
)

// ExtractPoppler shells out to pdftotext.
//
// It exists because the pure-Go path and this one fail on opposite documents,
// and measurably so. On a LaTeX two-column paper the Go path gets no glyph
// positions at all — every run reports X, W and FontSize of zero — so every
// word runs together. pdftotext reads it perfectly. On a Thai course handout
// the reverse: pdftotext drops the Thai characters entirely and returns only
// the digits, while the Go path reads it cleanly.
//
// Neither is the right default. See ExtractAuto.
func ExtractPoppler(path string, from, to int) ([]examgen.Page, error) {
	exe, err := findTool("pdftotext")
	if err != nil {
		return nil, err
	}

	args := []string{"-layout", "-enc", "UTF-8"}
	if from > 0 {
		args = append(args, "-f", strconv.Itoa(from))
	}
	if to > 0 {
		args = append(args, "-l", strconv.Itoa(to))
	}
	args = append(args, path, "-")

	out, err := exec.Command(exe, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("pdftotext: %w", err)
	}

	first := from
	if first < 1 {
		first = 1
	}

	// pdftotext separates pages with a form feed.
	var pages []examgen.Page
	for i, body := range strings.Split(string(out), "\f") {
		body = strings.TrimRight(repairThai(body), " \n")
		if strings.TrimSpace(body) == "" {
			continue
		}
		var kept []string
		for _, line := range strings.Split(body, "\n") {
			if isMojibake(line) {
				continue
			}
			kept = append(kept, strings.TrimRight(line, " "))
		}
		pages = append(pages, examgen.Page{Number: first + i, Text: strings.Join(kept, "\n")})
	}
	return pages, nil
}

// ExtractAuto runs both text extractors and picks one, by measured behaviour
// rather than preference.
//
// Two rules, both from observation on real files:
//
//  1. If the Go path recovered almost nothing while pdftotext did, the PDF gave
//     up no glyph geometry — every run reporting X, W and FontSize of zero,
//     which is what a LaTeX two-column paper does here. Nothing can be rebuilt
//     from that, so take pdftotext.
//
//  2. Otherwise, if the document is substantially Thai, take the Go path even
//     when pdftotext returns more text. Both extractors lose the NIKHAHIT of
//     SARA AM (ำ), but they lose it differently: the Go path leaves a space
//     where it was, which is a recoverable signal, while pdftotext leaves
//     nothing at all. "คำจำกัดความ" becomes "ค าจ ากัดความ" in one and
//     "คาจากัดความ" in the other. The first can be repaired; the second is a
//     different, plausible-looking Thai word and is silently wrong. Losing
//     layout is recoverable, losing letters is not.
//
// The chosen mode is returned so the caller can show it. Silently picking an
// extractor would hide exactly what this prototype needs to expose.
func ExtractAuto(path string, from, to int) ([]examgen.Page, string, error) {
	goPages, goErr := ExtractText(path, from, to)
	popPages, _ := ExtractPoppler(path, from, to)

	goScore := letterCount(goPages)
	popScore := letterCount(popPages)

	if goScore == 0 && popScore == 0 {
		if goErr != nil {
			return nil, "", goErr
		}
		// Neither found anything: a scan. The caller reports that.
		return goPages, "text", nil
	}

	// Rule 1 — the Go path got no usable geometry.
	if popScore > 0 && goScore*3 < popScore {
		return popPages, "poppler", nil
	}

	// Rule 2 — Thai belongs to the repairable extractor.
	if thaiRatio(goPages) > 0.2 || thaiRatio(popPages) > 0.2 {
		return goPages, "text (thai)", nil
	}

	if popScore > goScore {
		return popPages, "poppler", nil
	}
	return goPages, "text", nil
}

func letterCount(pages []examgen.Page) int {
	n := 0
	for _, p := range pages {
		for _, r := range p.Text {
			if unicode.IsLetter(r) {
				n++
			}
		}
	}
	return n
}

func thaiRatio(pages []examgen.Page) float64 {
	var thai, letters int
	for _, p := range pages {
		for _, r := range p.Text {
			if !unicode.IsLetter(r) {
				continue
			}
			letters++
			if isThai(r) {
				thai++
			}
		}
	}
	if letters == 0 {
		return 0
	}
	return float64(thai) / float64(letters)
}
