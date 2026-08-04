package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"protoexam/examgen"
)

type benchmarkCase struct {
	Name             string
	LessonContains   string
	Scope            string
	Directive        string
	ForceCalc        bool
	TargetSkill      string
	TargetDifficulty string
}

func benchmarkCases(selection string) ([]benchmarkCase, error) {
	applicationEasy := benchmarkCase{
		Name:             "application-easy",
		LessonContains:   "projectile",
		Scope:            "projectile motion worked example simple scenario",
		Directive:        "Generate application questions at easy level. Each question must apply one stated physics relationship to a simple new scenario with a changed value or condition and require a prediction, comparison, or outcome decision. Vary the relationship/target across questions. Do not ask for a name, definition, direct fact, force/component label, or unchanged property; do not copy a sentence. Set difficulty to easy and skill to application.",
		TargetSkill:      "application",
		TargetDifficulty: "easy",
	}
	applicationHard := benchmarkCase{
		Name:             "application-hard",
		LessonContains:   "projectile",
		Scope:            "projectile motion multi-step worked example nontrivial scenario",
		Directive:        "Generate application questions at hard level. Each question must use at least two given physics inputs or conditions and require at least two linked calculations/inferences before the answer; a one-step component/force fact is invalid. Vary the relationship/target across questions. Do not ask for a definition or direct recall, and do not repeat the same scenario/principle with different wording. Set difficulty to hard and skill to application.",
		TargetSkill:      "application",
		TargetDifficulty: "hard",
	}
	calculation := benchmarkCase{
		Name:           "calculation",
		LessonContains: "newton's second law",
		Scope:          "net force mass acceleration weight worked calculation",
		Directive:      "Generate calculation questions only. Use numerical values that appear in the passage or stem, show a solvable expression in calculation.expression, and set skill to calculation. Prefer applied physics scenarios over definition questions.",
		ForceCalc:      true,
		TargetSkill:    "calculation",
	}

	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "all":
		return []benchmarkCase{applicationEasy, applicationHard, calculation}, nil
	case "application-easy", "application_easy":
		return []benchmarkCase{applicationEasy}, nil
	case "application-hard", "application_hard":
		return []benchmarkCase{applicationHard}, nil
	case "calculation", "calc":
		return []benchmarkCase{calculation}, nil
	default:
		return nil, fmt.Errorf("--benchmark must be all, application-easy, application-hard, or calculation; got %q", selection)
	}
}

func findBenchmarkLesson(lessons []examgen.Lesson, contains string) (examgen.Lesson, error) {
	needle := strings.ToLower(strings.TrimSpace(contains))
	for _, lesson := range lessons {
		if strings.Contains(strings.ToLower(lesson.Title), needle) {
			return lesson, nil
		}
	}
	return examgen.Lesson{}, fmt.Errorf("benchmark lesson containing %q was not found", contains)
}

type benchmarkQuestion struct {
	examgen.Question
	Passed bool                 `json:"passed"`
	Gates  []examgen.GateResult `json:"gates"`
}

type benchmarkCaseResult struct {
	Name                string              `json:"name"`
	Lesson              string              `json:"lesson"`
	Budget              int                 `json:"budget"`
	Drafts              int                 `json:"drafts"`
	Accepted            int                 `json:"accepted"`
	PassRate            float64             `json:"pass_rate"`
	TargetDrafts        int                 `json:"target_drafts"`
	TargetAccepted      int                 `json:"target_accepted"`
	CalculationDrafts   int                 `json:"calculation_drafts"`
	CalculationAccepted int                 `json:"calculation_accepted"`
	Questions           []benchmarkQuestion `json:"questions"`
}

type benchmarkReport struct {
	Source string                `json:"source"`
	Pages  string                `json:"pages"`
	Suite  string                `json:"suite"`
	Cases  []benchmarkCaseResult `json:"cases"`
}

func runBenchmark(ctx context.Context, cfg config, outline *examgen.Outline, chunks []examgen.Chunk, deps examgen.Deps) error {
	cases, err := benchmarkCases(cfg.benchmark)
	if err != nil {
		return err
	}
	budget := cfg.budget
	if budget <= 0 {
		budget = 5
	}
	report := benchmarkReport{Source: cfg.pdfPath, Pages: cfg.pages, Suite: strings.ToLower(strings.TrimSpace(cfg.benchmark))}

	for _, benchmark := range cases {
		lesson, err := findBenchmarkLesson(outline.Lessons, benchmark.LessonContains)
		if err != nil {
			return err
		}
		opt := examgen.DefaultExamOptions()
		opt.Budget = budget
		opt.ForceCalc = benchmark.ForceCalc
		opt.Scope = benchmark.Scope
		opt.GenerationDirective = benchmark.Directive
		opt.PerChunk = 1
		opt.MaxChunkVisits = 16

		fmt.Printf("\n%sBENCHMARK %s — %s%s\n", bold, benchmark.Name, lesson.Title, reset)
		res, err := examgen.GenerateExam(ctx, outline, lesson, chunks, deps, opt)
		if err != nil {
			return fmt.Errorf("benchmark %s: %w", benchmark.Name, err)
		}
		caseResult := makeBenchmarkCaseResult(benchmark, res)
		report.Cases = append(report.Cases, caseResult)
		fmt.Printf("%s%s: %d/%d accepted (%.1f%%), target %d/%d, calculations %d/%d%s\n",
			dim, benchmark.Name, caseResult.Accepted, caseResult.Drafts, caseResult.PassRate*100,
			caseResult.TargetAccepted, caseResult.TargetDrafts,
			caseResult.CalculationAccepted, caseResult.CalculationDrafts, reset)
	}

	dir := scratchDir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "benchmark-"+sanitise(report.Suite)+".json")
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("\n%swrote %s%s\n", dim, path, reset)
	return nil
}

func makeBenchmarkCaseResult(benchmark benchmarkCase, res *examgen.ExamResult) benchmarkCaseResult {
	out := benchmarkCaseResult{
		Name:   benchmark.Name,
		Lesson: res.Lesson.Title,
		Budget: res.Budget,
		Drafts: len(res.Questions),
	}
	for _, q := range res.Questions {
		passed := q.Report != nil && q.Report.Passed()
		if passed {
			out.Accepted++
		}
		if q.Calculation != nil {
			out.CalculationDrafts++
			if passed {
				out.CalculationAccepted++
			}
		}
		target := strings.EqualFold(strings.TrimSpace(q.Skill), benchmark.TargetSkill)
		if benchmark.TargetDifficulty != "" {
			target = target && strings.EqualFold(strings.TrimSpace(q.Difficulty), benchmark.TargetDifficulty)
		}
		if target {
			out.TargetDrafts++
			if passed {
				out.TargetAccepted++
			}
		}
		reported := benchmarkQuestion{Question: q, Passed: passed}
		if q.Report != nil {
			reported.Gates = q.Report.Results
		}
		out.Questions = append(out.Questions, reported)
	}
	if out.Drafts > 0 {
		out.PassRate = float64(out.Accepted) / float64(out.Drafts)
	}
	return out
}
