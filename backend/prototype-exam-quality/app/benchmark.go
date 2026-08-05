package app

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
	Name                string
	LessonContains      string
	Scope               string
	Directive           string
	ForceCalc           bool
	TargetSkill         string
	TargetDifficulty    string
	RequiresCalculation bool
}

func benchmarkCases(selection string, lessonHint string, scopeHint string) ([]benchmarkCase, error) {
	if strings.TrimSpace(lessonHint) != "" {
		return genericBenchmarkCases(selection, lessonHint, scopeHint)
	}
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
	applicationMedium := benchmarkCase{
		Name:             "application-medium",
		LessonContains:   "projectile",
		Scope:            "projectile motion relationship applied comparison changed condition",
		Directive:        "Generate application questions at medium level. Each question must apply one stated physics relationship to a new scenario with one meaningful changed condition and require a comparison, prediction, or outcome decision. Do not ask for a definition, direct fact, or unchanged example. Set difficulty to medium and skill to application.",
		TargetSkill:      "application",
		TargetDifficulty: "medium",
	}
	calculation := benchmarkCase{
		Name:                "calculation",
		LessonContains:      "newton",
		Scope:               "net force mass acceleration weight worked calculation",
		Directive:           "Generate questions that require arithmetic. Use explicit numerical values that appear in the passage or stem and show a solvable expression in calculation.expression. Set requires_calculation=true and keep skill honest as understanding or application; never use calculation as a skill. The correct choice must be a decimal/integer numeric answer, never a radical, variable, or symbolic identity. Prefer applied physics scenarios over definition questions.",
		ForceCalc:           true,
		RequiresCalculation: true,
	}

	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "all":
		return []benchmarkCase{applicationEasy, applicationMedium, applicationHard, calculation}, nil
	case "application-easy", "application_easy":
		return []benchmarkCase{applicationEasy}, nil
	case "application-hard", "application_hard":
		return []benchmarkCase{applicationHard}, nil
	case "application-medium", "application_medium", "medium":
		return []benchmarkCase{applicationMedium}, nil
	case "calculation", "calc":
		return []benchmarkCase{calculation}, nil
	default:
		return nil, fmt.Errorf("--benchmark must be all, application-easy, application-hard, or calculation; got %q", selection)
	}
}

func genericBenchmarkCases(selection, lessonHint, scopeHint string) ([]benchmarkCase, error) {
	focus := strings.TrimSpace(scopeHint)
	if focus == "" {
		focus = strings.TrimSpace(lessonHint)
	}
	applicationEasy := benchmarkCase{
		Name:             "application-easy",
		LessonContains:   lessonHint,
		Scope:            focus + " source relationships examples simple new scenario",
		Directive:        "Generate application questions at easy level. Each question must apply one source-stated relationship, rule, mechanism, sequence, or condition to a genuinely new scenario with a changed value, entity, or condition and require a prediction, comparison, explanation, or outcome decision. Vary the target across questions. Do not ask for a name, definition, direct fact, unchanged property, or copied sentence. Set difficulty to easy and skill to application.",
		TargetSkill:      "application",
		TargetDifficulty: "easy",
	}
	applicationHard := benchmarkCase{
		Name:             "application-hard",
		LessonContains:   lessonHint,
		Scope:            focus + " source relationships multi-step nontrivial scenario",
		Directive:        "Generate application questions at hard level. Each question must use at least two given source-supported inputs, conditions, entities, or constraints and require at least two linked inferences, transformations, or decisions before the answer. A one-step fact, definition, label, or unchanged example is invalid. Vary the relationship and target across questions. Set difficulty to hard and skill to application.",
		TargetSkill:      "application",
		TargetDifficulty: "hard",
	}
	applicationMedium := benchmarkCase{
		Name:             "application-medium",
		LessonContains:   lessonHint,
		Scope:            focus + " source relationships applied comparison meaningful changed condition",
		Directive:        "Generate application questions at medium level. Each question must apply one source-stated relationship, rule, mechanism, sequence, or condition to a new scenario with one meaningful changed condition and require a comparison, prediction, explanation, or outcome decision. A one-step direct fact or unchanged example is invalid. Set difficulty to medium and skill to application.",
		TargetSkill:      "application",
		TargetDifficulty: "medium",
	}
	calculation := benchmarkCase{
		Name:                "calculation",
		LessonContains:      lessonHint,
		Scope:               focus + " numerical rule equation worked example",
		Directive:           "Generate questions that require arithmetic. Use explicit numerical values that appear in the passage or stem and show a solvable expression in calculation.expression. Set requires_calculation=true and keep skill honest as understanding or application; never use calculation as a skill. The correct choice must be a decimal/integer numeric answer, never a radical, variable, or symbolic identity. Prefer an applied scenario over a definition question.",
		ForceCalc:           true,
		RequiresCalculation: true,
	}

	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "all":
		return []benchmarkCase{applicationEasy, applicationMedium, applicationHard, calculation}, nil
	case "application-easy", "application_easy":
		return []benchmarkCase{applicationEasy}, nil
	case "application-hard", "application_hard":
		return []benchmarkCase{applicationHard}, nil
	case "application-medium", "application_medium", "medium":
		return []benchmarkCase{applicationMedium}, nil
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
	Name                string                 `json:"name"`
	Lesson              string                 `json:"lesson"`
	Skipped             bool                   `json:"skipped,omitempty"`
	SkipReason          string                 `json:"skip_reason,omitempty"`
	Budget              int                    `json:"budget"`
	Drafts              int                    `json:"drafts"`
	Accepted            int                    `json:"accepted"`
	PassRate            float64                `json:"pass_rate"`
	TargetDrafts        int                    `json:"target_drafts"`
	TargetAccepted      int                    `json:"target_accepted"`
	CalculationDrafts   int                    `json:"calculation_drafts"`
	CalculationAccepted int                    `json:"calculation_accepted"`
	DemandPassed        int                    `json:"demand_passed"`
	ChangedCondition    int                    `json:"changed_condition_passed"`
	NumericVerified     int                    `json:"numeric_verified"`
	WellFormedPassed    int                    `json:"well_formed_passed"`
	ContractRetries     int                    `json:"contract_retries"`
	Quality             *examgen.QualityReport `json:"quality,omitempty"`
	Questions           []benchmarkQuestion    `json:"questions"`
}

type benchmarkReport struct {
	Source string                `json:"source"`
	Pages  string                `json:"pages"`
	Suite  string                `json:"suite"`
	Cases  []benchmarkCaseResult `json:"cases"`
}

func runBenchmark(ctx context.Context, cfg config, outline *examgen.Outline, chunks []examgen.Chunk, deps examgen.Deps) error {
	cases, err := benchmarkCases(cfg.benchmark, cfg.benchmarkLesson, cfg.benchmarkScope)
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
		opt.PlanFirst = cfg.questionPlan
		opt.SetGeneration = cfg.setGeneration
		opt.SetCandidates = cfg.setCandidates
		opt.ContractPreflight = cfg.contractPreflight

		fmt.Printf("\n%sBENCHMARK %s — %s%s\n", bold, benchmark.Name, lesson.Title, reset)
		res, err := examgen.GenerateExam(ctx, outline, lesson, chunks, deps, opt)
		if err != nil {
			if benchmark.ForceCalc && strings.Contains(err.Error(), "no coverage slots") {
				caseResult := benchmarkCaseResult{
					Name:       benchmark.Name,
					Lesson:     lesson.Title,
					Budget:     budget,
					Skipped:    true,
					SkipReason: "source has no graph evidence that supports a numeric-required slot",
				}
				report.Cases = append(report.Cases, caseResult)
				fmt.Printf("%s%s: skipped — %s%s\n", yellow, benchmark.Name, caseResult.SkipReason, reset)
				continue
			}
			return fmt.Errorf("benchmark %s: %w", benchmark.Name, err)
		}
		caseResult := makeBenchmarkCaseResult(benchmark, res)
		report.Cases = append(report.Cases, caseResult)
		fmt.Printf("%s%s: %d/%d accepted (%.1f%%), target %d/%d, calc %d/%d, demand %d, changed %d, numeric %d, retries %d%s\n",
			dim, benchmark.Name, caseResult.Accepted, caseResult.Drafts, caseResult.PassRate*100,
			caseResult.TargetAccepted, caseResult.TargetDrafts,
			caseResult.CalculationAccepted, caseResult.CalculationDrafts,
			caseResult.DemandPassed, caseResult.ChangedCondition, caseResult.NumericVerified, caseResult.ContractRetries, reset)
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
		Name:            benchmark.Name,
		Lesson:          res.Lesson.Title,
		Budget:          res.Budget,
		Quality:         res.Quality,
		Drafts:          len(res.Questions),
		ContractRetries: res.SetContractRetries,
	}
	for _, q := range res.Questions {
		passed := q.Report != nil && q.Report.Passed()
		if passed {
			out.Accepted++
		}
		if q.NeedsCalculation() {
			out.CalculationDrafts++
			if passed {
				out.CalculationAccepted++
			}
		}
		isDemandTarget := strings.EqualFold(strings.TrimSpace(q.Skill), "application") && (strings.EqualFold(strings.TrimSpace(q.Difficulty), "medium") || strings.EqualFold(strings.TrimSpace(q.Difficulty), "hard"))
		if isDemandTarget && gatePassed(q.Report, examgen.GateDemand) {
			out.DemandPassed++
		}
		if isDemandTarget && strings.TrimSpace(q.ChangedCondition) != "" {
			out.ChangedCondition++
		}
		if gatePassed(q.Report, examgen.GateArithmetic) && q.NeedsCalculation() {
			out.NumericVerified++
		}
		if gatePassed(q.Report, examgen.GateWellFormed) {
			out.WellFormedPassed++
		}
		target := true
		if benchmark.TargetSkill != "" {
			target = strings.EqualFold(strings.TrimSpace(q.Skill), benchmark.TargetSkill)
		}
		if benchmark.TargetDifficulty != "" {
			target = target && strings.EqualFold(strings.TrimSpace(q.Difficulty), benchmark.TargetDifficulty)
		}
		if benchmark.RequiresCalculation {
			target = target && q.NeedsCalculation()
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

func gatePassed(report *examgen.GateReport, wanted examgen.GateName) bool {
	if report == nil {
		return false
	}
	for _, result := range report.Results {
		if result.Gate == wanted {
			return result.Pass
		}
	}
	return false
}
