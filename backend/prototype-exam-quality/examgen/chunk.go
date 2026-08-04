package examgen

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Page is one page of extracted text, however it was extracted.
type Page struct {
	Number int
	Text   string
}

// ChunkOptions controls the splitter.
type ChunkOptions struct {
	// TargetRunes is the size we aim for. Runes, not bytes: Thai characters are
	// 3 bytes each in UTF-8, so a byte budget would silently produce Thai chunks
	// a third the size of English ones.
	TargetRunes int
	// OverlapRunes is carried from the tail of one chunk into the head of the
	// next so a fact straddling a boundary is still answerable from one chunk.
	OverlapRunes int
}

// DefaultChunkOptions is sized for a 4B model with a modest context window.
func DefaultChunkOptions() ChunkOptions {
	return ChunkOptions{TargetRunes: 1200, OverlapRunes: 150}
}

// Chunk splits pages into chunks, never merging across a page boundary so that
// the page number on a chunk is always exactly true.
//
// Splitting prefers, in order: blank lines, single newlines, sentence-ending
// punctuation. It deliberately does not split on spaces — Thai does not put
// spaces between words, and a space-based splitter cuts Thai mid-word.
func ChunkPages(pages []Page, opt ChunkOptions) []Chunk {
	if opt.TargetRunes <= 0 {
		opt = DefaultChunkOptions()
	}
	if opt.OverlapRunes >= opt.TargetRunes {
		opt.OverlapRunes = opt.TargetRunes / 8
	}

	var out []Chunk
	for _, p := range pages {
		text := normalise(p.Text)
		if strings.TrimSpace(text) == "" {
			continue
		}
		runes := []rune(text)
		start := 0
		for start < len(runes) {
			end := start + opt.TargetRunes
			if end >= len(runes) {
				end = len(runes)
			} else {
				end = backOffToBoundary(runes, start, end)
			}

			body := strings.TrimSpace(string(runes[start:end]))
			if body != "" {
				out = append(out, Chunk{
					ID:         fmt.Sprintf("p%d-c%d", p.Number, len(out)),
					Page:       p.Number,
					StartOff:   start,
					EndOff:     end,
					Text:       body,
					SourceRole: classifySourceRole(body),
				})
			}

			if end >= len(runes) {
				break
			}
			next := end - opt.OverlapRunes
			if next <= start {
				// Overlap would not advance; give up on overlap for this step
				// rather than loop forever.
				next = end
			}
			start = next
		}
	}
	return out
}

// classifySourceRole uses only explicit section headings. The textbook's
// pre-learning answer key contains intentionally true/false statements, so
// those statements are not safe evidence even though they are verbatim text.
// Everything else stays usable; semantic quality is not inferred here.
func classifySourceRole(text string) SourceRole {
	compact := strings.Join(strings.Fields(text), "")
	if strings.Contains(compact, "ตรวจสอบความรู้ก่อนเรียน") {
		return SourceRolePrelearningCheck
	}
	return SourceRoleCore
}

// backOffToBoundary walks backwards from end looking for a natural break,
// giving up and returning the hard limit if it would cut off more than a
// quarter of the chunk.
func backOffToBoundary(runes []rune, start, end int) int {
	floor := start + (end-start)*3/4

	// Blank line is the best break we can hope for.
	if i := lastIndexOfPair(runes, start, end, '\n', '\n'); i > floor {
		return i + 1
	}
	if i := lastIndexOfRune(runes, floor, end, '\n'); i > floor {
		return i + 1
	}
	for _, r := range []rune{'.', '。', '!', '?', ';', ':'} {
		if i := lastIndexOfRune(runes, floor, end, r); i > floor {
			return i + 1
		}
	}
	return end
}

func lastIndexOfRune(runes []rune, from, to int, want rune) int {
	for i := to - 1; i >= from; i-- {
		if runes[i] == want {
			return i
		}
	}
	return -1
}

func lastIndexOfPair(runes []rune, from, to int, a, b rune) int {
	for i := to - 1; i > from; i-- {
		if runes[i] == b && runes[i-1] == a {
			return i
		}
	}
	return -1
}

// normalise collapses the whitespace damage typical of PDF extraction without
// destroying paragraph structure, which is the only structural signal the
// chunker has.
func normalise(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, " ", " ")
	// Collapse runs of 3+ newlines to exactly 2 so "blank line" stays meaningful.
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if lastSpace {
				continue
			}
			lastSpace = true
			b.WriteRune(' ')
			continue
		}
		lastSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// RuneLen is a small helper so callers reporting sizes report runes, matching
// how the chunker thinks.
func RuneLen(s string) int { return utf8.RuneCountInString(s) }

// ChunkByID indexes chunks for the gates, which need to look up the chunk a
// question claims to have come from.
func ChunkByID(chunks []Chunk) map[string]Chunk {
	m := make(map[string]Chunk, len(chunks))
	for _, c := range chunks {
		m[c.ID] = c
	}
	return m
}
