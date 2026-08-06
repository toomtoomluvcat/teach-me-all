package app

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"protoexam/examgen"
)

// Throwaway terminal rendering. Nothing in here is meant to survive the
// prototype — the point is to see state, not to be pretty.

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	cyan   = "\x1b[36m"
)

func clear() { fmt.Print("\x1b[2J\x1b[H") }

func header(s string) {
	fmt.Printf("%s%s%s\n%s%s%s\n", bold, s, reset, dim, strings.Repeat("─", 72), reset)
}

func keys(pairs ...string) {
	fmt.Printf("\n%s%s%s\n", dim, strings.Repeat("─", 72), reset)
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, fmt.Sprintf("%s[%s]%s %s%s%s", bold, pairs[i], reset, dim, pairs[i+1], reset))
	}
	fmt.Println(strings.Join(parts, "  "))
}

// renderExtraction shows what came out of the PDF. This screen exists because
// bad extraction and a bad model look identical downstream, and this is the
// only place you can tell them apart.
func renderExtraction(pdfPath, mode string, pages []examgen.Page, chunks []examgen.Chunk, waitForInput bool) {
	clear()
	header("STEP 1 — extracted text")

	total := 0
	for _, p := range pages {
		total += examgen.RuneLen(p.Text)
	}
	fmt.Printf("%ssource%s      %s\n", bold, reset, pdfPath)
	fmt.Printf("%smode%s        %s\n", bold, reset, mode)
	fmt.Printf("%spages%s       %d\n", bold, reset, len(pages))
	fmt.Printf("%sruns%s        %d runes, %d chunks\n\n", bold, reset, total, len(chunks))

	fmt.Printf("%sRead this before continuing. If it is garbled, stop — the model is not the problem.%s\n\n", yellow, reset)

	shown := 0
	for _, p := range pages {
		if shown >= 2 {
			break
		}
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		fmt.Printf("%s── page %d ──%s\n", cyan, p.Number, reset)
		fmt.Println(excerpt(p.Text, 600))
		fmt.Println()
		shown++
	}

	if waitForInput {
		keys("enter", "continue", "q + enter", "quit")
	}
}

// renderOutline lists the lessons pass 1 produced.
func renderOutline(o *examgen.Outline, chunks []examgen.Chunk) {
	clear()
	header("STEP 2 — course outline (pass 1)")
	fmt.Printf("%s%s%s\n\n", bold, o.CourseTitle, reset)

	orphans := 0
	for _, c := range chunks {
		if c.LessonID == "" {
			orphans++
		}
	}

	for i, l := range o.Lessons {
		fmt.Printf("%s%2d.%s %s\n", bold, i+1, reset, l.Title)
		fmt.Printf("     %s%s%s\n", dim, l.Summary, reset)
		fmt.Printf("     %s%d concepts · %d chunks · budget %d questions%s\n", dim, len(l.ConceptIDs), len(l.ChunkIDs), l.QuestionBudget, reset)
	}
	if orphans > 0 {
		fmt.Printf("\n%s%d chunks had no teaching concept (front matter or page furniture)%s\n",
			dim, orphans, reset)
	}

	keys("1-"+fmt.Sprint(len(o.Lessons)), "generate exam for that lesson", "q", "quit")
}

// renderQuestion shows one question and every gate result against it.
func renderQuestion(res *examgen.ExamResult, idx int) {
	clear()
	q := res.Questions[idx]

	verdict := green + "PASSED" + reset
	if q.Report != nil && !q.Report.Passed() {
		verdict = red + "FAILED" + reset
	}
	header(fmt.Sprintf("question %d of %d — %s", idx+1, len(res.Questions), verdict))

	fmt.Printf("%s%s%s\n\n", bold, q.Stem, reset)
	for i, c := range q.Choices {
		mark := "  "
		if c.IsCorrect {
			mark = green + "✓ " + reset
		}
		fmt.Printf("  %s%d. %s\n", mark, i+1, c.Content)
	}

	fmt.Printf("\n%swhy%s          %s\n", dim, reset, q.Explanation)
	fmt.Printf("%squote%s        %s%q%s\n", dim, reset, dim, excerpt(q.SourceQuote, 160), reset)
	fmt.Printf("%sfrom%s         %s chunk %s · %s · %s\n", dim, reset, dim, q.ChunkID, q.Difficulty, q.Skill+reset)
	if q.Calculation != nil {
		fmt.Printf("%scalculation%s  %s = %g\n", dim, reset, q.Calculation.Expression, q.Calculation.Expected)
	}

	fmt.Printf("\n%sgates%s\n", bold, reset)
	if q.Report != nil {
		for _, r := range q.Report.Results {
			icon := green + "pass" + reset
			if !r.Pass {
				icon = red + "FAIL" + reset
			}
			kind := "judge"
			if r.Deterministic {
				kind = "go   "
			}
			fmt.Printf("  %s  %s%-18s%s %s%s%s\n", icon, bold, r.Gate, reset, dim, kind, reset)
			fmt.Printf("        %s%s%s\n", dim, wrap(r.Reason, 62, "        "), reset)
		}
	}

	keys("n", "next", "p", "previous", "f", "next failure", "s", "summary", "q", "quit")
}

// renderSummary is the number the prototype exists to produce.
func renderSummary(res *examgen.ExamResult) {
	clear()
	header("SUMMARY — " + res.Lesson.Title)

	fmt.Printf("%sbudget%s          %d questions (set by the model from the material)\n", bold, reset, res.Budget)
	fmt.Printf("%sgenerated%s       %d\n", bold, reset, len(res.Questions))
	fmt.Printf("%spassed all gates%s %d\n", bold, reset, len(res.Passed))
	fmt.Printf("%spass rate%s       %s%.0f%%%s\n", bold, reset, bold, res.PassRate()*100, reset)
	if res.SetCandidates > 1 {
		fmt.Printf("%sset candidates%s  %d (best score %d)\n", bold, reset, res.SetCandidates, res.SelectedSetScore)
	}
	if res.Ceiling {
		fmt.Printf("\n%sCeiling reached: this material supports %d questions, not %d.%s\n",
			yellow, len(res.Passed), res.Budget, reset)
		fmt.Printf("%sThat is the honest answer, not a bug. Nothing was pulled in from outside your files.%s\n", dim, reset)
	}

	failures := res.FailuresByGate()
	if len(failures) > 0 {
		fmt.Printf("\n%sfailures by gate%s  %s(aim the next prompt change at the biggest one)%s\n", bold, reset, dim, reset)
		// Read determinism off the results rather than keeping a second list of
		// which gates are which — that list drifts the moment a gate is added.
		deterministic := map[examgen.GateName]bool{}
		for _, q := range res.Questions {
			if q.Report == nil {
				continue
			}
			for _, r := range q.Report.Results {
				if r.Deterministic {
					deterministic[r.Gate] = true
				}
			}
		}

		names := make([]examgen.GateName, 0, len(failures))
		for g := range failures {
			names = append(names, g)
		}
		sort.Slice(names, func(a, b int) bool { return failures[names[a]] > failures[names[b]] })
		for _, g := range names {
			trust := dim + "judge — advisory" + reset
			if deterministic[g] {
				trust = dim + "go — deterministic" + reset
			}
			fmt.Printf("  %-20s %3d   %s\n", g, failures[g], trust)
		}
	}

	fmt.Printf("\n%sNext: read 20 passing questions yourself, then run the same PDF through NotebookLM and compare.%s\n", dim, reset)
	fmt.Printf("%sWrite what you conclude into VERDICT.md — that is what closes this prototype.%s\n", dim, reset)

	keys("enter", "back to questions", "q", "quit")
}

// safeProgress serialises progress writes. Once the pipeline runs calls
// concurrently, several goroutines reach for the same terminal line.
func safeProgress() examgen.Progress {
	var mu sync.Mutex
	return func(stage string, done, total int, note string) {
		mu.Lock()
		defer mu.Unlock()
		renderProgress(stage, done, total, note)
	}
}

func renderProgress(stage string, done, total int, note string) {
	if total > 0 {
		fmt.Printf("\r\x1b[K%s%-16s%s %s%3d/%-3d%s %s%s%s", bold, stage, reset, cyan, done, total, reset, dim, note, reset)
		return
	}
	fmt.Printf("\r\x1b[K%s%-16s%s %s%s%s", bold, stage, reset, dim, note, reset)
}

func excerpt(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + dim + " …" + reset
}

func wrap(s string, width int, indent string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	line := 0
	for i, w := range words {
		if line > 0 && line+len(w)+1 > width {
			b.WriteString("\n" + indent)
			line = 0
		} else if i > 0 {
			b.WriteString(" ")
			line++
		}
		b.WriteString(w)
		line += len(w)
	}
	return b.String()
}
