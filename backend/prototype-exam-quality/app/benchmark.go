package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

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
	// Accept a comma-separated list ("recall,understanding,application-hard")
	// so a benchmark can skip cases that are slow or unsupported for a given
	// source (for example calculation on a non-numeric humanities chapter).
	// Each part is resolved through the same single-selection logic below and
	// the results are merged in order, dropping duplicates.
	if strings.Contains(selection, ",") {
		var merged []benchmarkCase
		seen := map[string]bool{}
		for _, part := range strings.Split(selection, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			resolved, err := benchmarkCases(part, lessonHint, scopeHint)
			if err != nil {
				return nil, err
			}
			for _, c := range resolved {
				if seen[c.Name] {
					continue
				}
				seen[c.Name] = true
				merged = append(merged, c)
			}
		}
		return merged, nil
	}
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
	analysis := benchmarkCase{
		Name:             "analysis",
		LessonContains:   "newton",
		Scope:            "force friction motion two linked relationships combined",
		Directive:        "Generate analysis questions. Each question must combine two distinct physics relationships or facts from two different, clearly separate parts of the passage -- not the same relationship applied twice to two numbers, and not one relationship restated. The answer must genuinely need both supporting facts; a student who only had one could not reach it. Set difficulty to hard and skill to analysis.",
		TargetSkill:      "analysis",
		TargetDifficulty: "hard",
	}

	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "all":
		return []benchmarkCase{applicationEasy, applicationMedium, applicationHard, calculation, analysis}, nil
	case "application-easy", "application_easy":
		return []benchmarkCase{applicationEasy}, nil
	case "application-hard", "application_hard":
		return []benchmarkCase{applicationHard}, nil
	case "application-medium", "application_medium", "medium":
		return []benchmarkCase{applicationMedium}, nil
	case "calculation", "calc":
		return []benchmarkCase{calculation}, nil
	case "analysis":
		return []benchmarkCase{analysis}, nil
	default:
		return nil, fmt.Errorf("--benchmark must be all, application-easy, application-hard, calculation, or analysis; got %q", selection)
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
	analysisEasy := benchmarkCase{
		Name:             "analysis-easy",
		LessonContains:   lessonHint,
		Scope:            focus + " two directly-stated closely-linked facts combined from different parts of the source",
		Directive:        "Generate analysis questions at easy level. Each question must combine two distinct source-stated relationships, mechanisms, or facts from two different, clearly separate parts of the passage -- not the same relationship applied twice, and not one relationship restated. Pick two facts that are directly stated and closely linked, so combining them is straightforward once both are known. The answer must genuinely need both supporting facts; a student who only had one could not reach it. Set difficulty to easy and skill to analysis.",
		TargetSkill:      "analysis",
		TargetDifficulty: "easy",
	}
	analysisMedium := benchmarkCase{
		Name:             "analysis-medium",
		LessonContains:   lessonHint,
		Scope:            focus + " two related facts combined with one meaningful distinction from different parts of the source",
		Directive:        "Generate analysis questions at medium level. Each question must combine two distinct source-stated relationships, mechanisms, or facts from two different, clearly separate parts of the passage -- not the same relationship applied twice, and not one relationship restated. The combination should require noticing one meaningful distinction between the two facts, not just concatenating them. The answer must genuinely need both supporting facts; a student who only had one could not reach it. Set difficulty to medium and skill to analysis.",
		TargetSkill:      "analysis",
		TargetDifficulty: "medium",
	}
	analysisHard := benchmarkCase{
		Name:             "analysis-hard",
		LessonContains:   lessonHint,
		Scope:            focus + " two distinct relationships or facts combined from different parts of the source",
		Directive:        "Generate analysis questions. Each question must combine two distinct source-stated relationships, mechanisms, or facts from two different, clearly separate parts of the passage -- not the same relationship applied twice, and not one relationship restated. The answer must genuinely need both supporting facts; a student who only had one could not reach it. Set difficulty to hard and skill to analysis.",
		TargetSkill:      "analysis",
		TargetDifficulty: "hard",
	}
	recall := benchmarkCase{
		Name:           "recall",
		LessonContains: lessonHint,
		Scope:          focus + " named facts definitions values stated directly in the source",
		Directive:      "Generate recall questions. Each question must ask for one fact, name, value, or definition stated directly in the passage. Do not ask the student to apply, compare, or combine anything. Set skill to recall.",
		TargetSkill:    "recall",
	}
	understanding := benchmarkCase{
		Name:           "understanding",
		LessonContains: lessonHint,
		Scope:          focus + " interpret explain classify compare or predict from a source relationship",
		Directive:      "Generate understanding questions. Each question must ask the student to interpret, explain, classify, compare, or predict from a relationship stated in the source -- not merely name a fact (that is recall), and not apply the relationship to a new/changed scenario (that is application). Set skill to understanding.",
		TargetSkill:    "understanding",
	}

	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "all":
		return []benchmarkCase{recall, understanding, applicationEasy, applicationMedium, applicationHard, calculation, analysisEasy, analysisMedium, analysisHard}, nil
	case "recall":
		return []benchmarkCase{recall}, nil
	case "understanding":
		return []benchmarkCase{understanding}, nil
	case "application-easy", "application_easy":
		return []benchmarkCase{applicationEasy}, nil
	case "application-hard", "application_hard":
		return []benchmarkCase{applicationHard}, nil
	case "application-medium", "application_medium", "medium":
		return []benchmarkCase{applicationMedium}, nil
	case "calculation", "calc":
		return []benchmarkCase{calculation}, nil
	case "analysis", "analysis-hard", "analysis_hard":
		return []benchmarkCase{analysisHard}, nil
	case "analysis-easy", "analysis_easy":
		return []benchmarkCase{analysisEasy}, nil
	case "analysis-medium", "analysis_medium":
		return []benchmarkCase{analysisMedium}, nil
	default:
		return nil, fmt.Errorf("--benchmark must be all, recall, understanding, application-easy, application-medium, application-hard, calculation, analysis-easy, analysis-medium, or analysis-hard; got %q", selection)
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
	// LengthBias is advisory, not a gate: true when the keyed choice is
	// disproportionately longer than the distractors, which makes the answer
	// decodable by length. It does not set Passed=false because a verbose
	// correct option is not a deterministic defect — a human reviewer should
	// decide. Reported so benchmark reports surface the answer-length tell.
	LengthBias bool `json:"length_bias,omitempty"`
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
	LengthBiasPassed    int                    `json:"length_bias_passed"`
	LengthBiasTotal     int                    `json:"length_bias_total"`
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
	// A comma-separated selection produces an unreadable suite name if it is
	// used verbatim (sanitise truncates it mid-word). Use "first,second" or
	// "first-et-al" so report filenames stay readable.
	suite := strings.ToLower(strings.TrimSpace(cfg.benchmark))
	if strings.Contains(suite, ",") {
		parts := strings.Split(suite, ",")
		if len(parts) > 2 {
			suite = strings.TrimSpace(parts[0]) + "-et-al"
		} else {
			suite = strings.TrimSpace(parts[0]) + "," + strings.TrimSpace(parts[1])
		}
	}
	report := benchmarkReport{Source: cfg.pdfPath, Pages: cfg.pages, Suite: suite}

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
		opt.SetCandidates = cfg.setCandidates
		opt.ContractPreflight = cfg.contractPreflight
		opt.StopOnFullSet = cfg.stopOnFullSet

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
		fmt.Printf("%s%s: %d/%d accepted (%.1f%%), target %d/%d, calc %d/%d, demand %d, changed %d, numeric %d, retries %d, lenbias %d/%d%s\n",
			dim, benchmark.Name, caseResult.Accepted, caseResult.Drafts, caseResult.PassRate*100,
			caseResult.TargetAccepted, caseResult.TargetDrafts,
			caseResult.CalculationAccepted, caseResult.CalculationDrafts,
			caseResult.DemandPassed, caseResult.ChangedCondition, caseResult.NumericVerified, caseResult.ContractRetries,
			caseResult.LengthBiasPassed, caseResult.LengthBiasTotal, reset)
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
		isDemandTarget := strings.EqualFold(strings.TrimSpace(q.Skill), "analysis") ||
			(strings.EqualFold(strings.TrimSpace(q.Skill), "application") && (strings.EqualFold(strings.TrimSpace(q.Difficulty), "medium") || strings.EqualFold(strings.TrimSpace(q.Difficulty), "hard")))
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
		if lengthBiasRatio(q) > 0 {
			out.LengthBiasTotal++
			reported.LengthBias = true
			if passed {
				out.LengthBiasPassed++
			}
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

// lengthBiasRatio returns the correct-choice length divided by the average
// wrong-choice length, or 0 when the tell does not apply (no key, fewer than
// two choices, or distractors too short to matter). This mirrors the intended
// gate A check but stays advisory: it only reports the bias so a reviewer can
// decide, it never rejects the question. The 1.4 threshold and the 25-rune
// prose floor match what gate A used.
func lengthBiasRatio(q examgen.Question) float64 {
	idx := q.CorrectIndex()
	if len(q.Choices) < 2 || idx < 0 {
		return 0
	}
	var wrongSum, wrongN int
	for i, c := range q.Choices {
		if i == idx {
			continue
		}
		wrongSum += utf8.RuneCountInString(strings.TrimSpace(c.Content))
		wrongN++
	}
	if wrongN == 0 {
		return 0
	}
	avgWrong := float64(wrongSum) / float64(wrongN)
	if avgWrong < 25 {
		return 0
	}
	correctLen := utf8.RuneCountInString(strings.TrimSpace(q.Choices[idx].Content))
	ratio := float64(correctLen) / avgWrong
	if ratio <= 1.4 {
		return 0
	}
	return ratio
}
