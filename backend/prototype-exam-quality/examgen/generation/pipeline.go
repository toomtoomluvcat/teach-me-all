package generation

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Generator is the model-backed half of generation. Advisory quality review is
// kept outside the acceptance loop so deterministic QC stays cheap and clear.
type Generator interface {
	Topics(ctx context.Context, c Chunk) (ChunkTopics, error)
	Outline(ctx context.Context, graph EvidenceGraph) (*Outline, []LessonConcepts, error)
	Questions(ctx context.Context, lesson Lesson, graph *EvidenceGraph, c Chunk, feedback []RejectedDraft, want int, forceCalc bool) ([]Question, error)
}

// QuestionSetGenerator receives the whole lesson context and a validated
// coverage contract in one call. It is optional so the original chunk path
// remains usable for small models and test doubles.
type QuestionSetGenerator interface {
	QuestionsSet(ctx context.Context, lesson Lesson, graph *EvidenceGraph, chunks []Chunk, contract CoverageContract, feedback []RejectedDraft, forceCalc bool) ([]Question, error)
}

// RejectedDraft is compact negative memory for later generation calls. It
// describes what failed without asking the model to salvage the same question.
type RejectedDraft struct {
	Stem     string
	Choices  []string
	Failures []GateResult
}

// LessonConcepts is the graph membership returned by the reduce step. Stable
// concept IDs replace free-text topic titles as the join key.
type LessonConcepts struct {
	LessonID   string
	ConceptIDs []string
}

// Embedder turns text into vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// TopicBatcher is an optional provider optimisation. It is deliberately not
// part of Generator: Ollama keeps the measured one-call-per-chunk map path,
// while Gemini can map all chunks in one larger-context request.
type TopicBatcher interface {
	BatchTopics(ctx context.Context, chunks []Chunk, progress Progress) ([]ChunkTopics, error)
}

// Progress lets the TUI show what is happening during the slow parts. The logic
// package never prints; it calls this.
type Progress func(stage string, done, total int, note string)

func (p Progress) report(stage string, done, total int, note string) {
	if p != nil {
		p(stage, done, total, note)
	}
}

// Deps bundles everything the pipeline needs from the outside world.
type Deps struct {
	Gen          Generator
	CompileGraph bool
	TopicBatcher TopicBatcher
	Eval         Evaluator
	Embedder     Embedder
	Quality      QualityGrader
	Log          Progress
	// Parallel is how many model calls may be in flight at once. Ollama serves
	// concurrent requests from one loaded model when OLLAMA_NUM_PARALLEL allows
	// it, and the calls this pipeline makes are independent of each other, so
	// the wall clock drops without changing a single verdict. 1 disables it.
	Parallel int
	// KeepAllTopics turns off apparatus filtering for a source that really is
	// mostly exercises and rubrics. It is opt-out on purpose: the zero value
	// filters, and a book that trips the guard below makes a human decide.
	KeepAllTopics bool
}

func (d Deps) slots() int {
	if d.Parallel < 1 {
		return 1
	}
	return d.Parallel
}

// ExamOptions controls pass 2.
type ExamOptions struct {
	// Budget overrides the model's own question_budget when > 0.
	Budget int
	// ForceCalc restricts generation to calculation questions.
	ForceCalc bool
	// Scope is an optional free-text focus. When set, chunks are ranked by
	// similarity to it instead of to the lesson title.
	Scope string
	// GenerationDirective is an optional benchmark-only instruction appended to
	// the question prompt. Normal production generation leaves it empty.
	GenerationDirective string
	// PerChunk is how many questions to ask for from one chunk at a time.
	PerChunk int
	// PlanFirst asks an optional LessonPlanner for a set-level coverage plan
	// before generation. Each subsequent call receives its current slot and the
	// already-used targets.
	PlanFirst bool
	// SetGeneration sends the whole lesson context and a graph-derived coverage
	// contract to a set-capable generator in one call.
	SetGeneration bool
	// SetCandidates asks the set-capable generator for independent candidate
	// sets. The deterministic selector keeps the best accepted/diverse set.
	SetCandidates int
	// ContractPreflight repairs/drops deterministic slot defects before asking
	// the model to write the set. It costs no model call.
	ContractPreflight bool
	// MaxChunkVisits caps total work so a lesson that cannot fill its budget
	// stops instead of grinding through the whole book.
	MaxChunkVisits int
}

// DefaultExamOptions is sized so a run finishes in minutes, not hours.
func DefaultExamOptions() ExamOptions {
	return ExamOptions{PerChunk: 2, SetCandidates: 1, ContractPreflight: true, MaxChunkVisits: 24}
}

// ExamResult is everything one generation run produced, kept whole. Questions
// that failed a gate are in Questions with a report explaining why; they are
// simply absent from Passed.
type ExamResult struct {
	Lesson             Lesson
	Budget             int
	Plan               *QuestionPlan     `json:"plan,omitempty"`
	Contract           *CoverageContract `json:"contract,omitempty"`
	Questions          []Question
	Passed             []Question
	ChunkVisits        int
	SetCandidates      int `json:"set_candidates,omitempty"`
	SetContractRetries int `json:"set_contract_retries,omitempty"`
	SelectedSetScore   int `json:"selected_set_score,omitempty"`
	// Quality is advisory semantic review for the selected set. It is not a
	// hard acceptance gate and may be nil when the grader was unavailable.
	Quality *QualityReport `json:"quality,omitempty"`
	// ToppedUpFrom names lessons other than this one that were drawn on.
	ToppedUpFrom []string
	// Ceiling is true when the run stopped short of Budget because the material
	// ran out. This is the honest answer, not a failure.
	Ceiling bool
}

// GenerateExam runs pass 2 for one lesson: generate, gate, and top up from
// elsewhere in the user's own document when questions fail.
//
// It never reaches outside the uploaded material. When the material cannot
// support the budget it stops and sets Ceiling.
func GenerateExam(ctx context.Context, outline *Outline, lesson Lesson, chunks []Chunk, d Deps, opt ExamOptions) (*ExamResult, error) {
	if opt.PerChunk <= 0 {
		opt = DefaultExamOptions()
	}
	budget := lesson.QuestionBudget
	if opt.Budget > 0 {
		budget = opt.Budget
	}
	if budget <= 0 {
		budget = 5
	}
	byID := ChunkByID(chunks)
	res := &ExamResult{Lesson: lesson, Budget: budget}

	primary := chunksFor(lesson.ChunkIDs, byID)
	query := lesson.Title + " " + lesson.Summary
	if opt.Scope != "" {
		query = opt.Scope
	}
	primary = rankByRelevance(ctx, primary, query, d)
	if opt.SetGeneration {
		return generateExamSet(ctx, outline, lesson, chunks, d, opt, budget)
	}

	var plan *QuestionPlan
	if opt.PlanFirst {
		planner, ok := d.Gen.(LessonPlanner)
		if !ok {
			return nil, fmt.Errorf("question plan requested but generator has no lesson planner")
		}
		planned, err := planner.PlanQuestions(ctx, lesson, outline.EvidenceGraph, primary, budget)
		if err != nil {
			return nil, fmt.Errorf("plan questions: %w", err)
		}
		if len(planned.Slots) == 0 {
			return nil, fmt.Errorf("question planner returned no usable slots")
		}
		plan = &planned
		res.Plan = plan
		d.Log.report("question-plan", len(plan.Slots), budget, "lesson-level coverage slots")
	}

	// Sibling chunks are the top-up pool: still the user's own document, just
	// from a different lesson.
	var siblings []Chunk
	for _, c := range chunks {
		if c.LessonID != "" && c.LessonID != lesson.ID {
			siblings = append(siblings, c)
		}
	}
	siblings = rankByRelevance(ctx, siblings, query, d)

	lessonName := map[string]string{}
	for _, l := range outline.Lessons {
		lessonName[l.ID] = l.Title
	}

	pool := append(append([]Chunk{}, primary...), siblings...)
	toppedUp := map[string]bool{}

	// acceptedVecs runs in lockstep with res.Passed so the duplicate check can
	// compare against everything already in the exam.
	var acceptedVecs [][]float32
	var feedback []RejectedDraft
	planIndex := 0

	for _, c := range pool {
		if len(res.Passed) >= budget || res.ChunkVisits >= opt.MaxChunkVisits {
			break
		}
		res.ChunkVisits++
		want := budget - len(res.Passed)
		if want > opt.PerChunk {
			want = opt.PerChunk
		}
		if plan != nil {
			// A plan slot is a single semantic target. Keeping this one question
			// per call makes the experiment measure shared planning rather than
			// allowing two questions to consume the same slot.
			want = 1
		}

		note := fmt.Sprintf("page %d", c.Page)
		if len(feedback) > 0 {
			note += fmt.Sprintf(" · avoiding %d rejected pattern(s)", len(feedback))
		}
		d.Log.report("generate", len(res.Passed), budget, note)

		generationChunk := c
		generationChunk.GenerationDirective = opt.GenerationDirective
		if plan != nil {
			generationChunk.GenerationDirective = joinDirectives(
				generationChunk.GenerationDirective,
				questionPlanDirective(plan, planIndex, res.Passed),
			)
		}
		qs, err := d.Gen.Questions(ctx, lesson, outline.EvidenceGraph, generationChunk, feedback, want, opt.ForceCalc)
		if err != nil {
			return nil, fmt.Errorf("generate from chunk %s: %w", c.ID, err)
		}

		// Run all cheap checks before touching the embedder. A rejected question
		// cannot become accepted, so embedding its stem only causes an avoidable
		// model swap. Batch the remaining stems: one chunk commonly returns two
		// questions, and two one-item embed calls would evict and reload bge-m3
		// twice on a small GPU.
		cheap := make([]*GateReport, len(qs))
		eligible := make([]Question, 0, len(qs))
		eligibleAt := make([]int, 0, len(qs))
		for i, q := range qs {
			q.ChunkID = c.ID
			cheap[i] = RunCheapGates(q, c, d.Eval)
			if len(cheap[i].Failures()) == 0 && strings.TrimSpace(q.Stem) != "" {
				eligible = append(eligible, q)
				eligibleAt = append(eligibleAt, i)
			}
		}
		vectors := stemVectors(ctx, eligible, d)
		vectorsByQuestion := make([][]float32, len(qs))
		if len(vectors) == len(eligible) {
			for i, questionAt := range eligibleAt {
				vectorsByQuestion[questionAt] = vectors[i]
			}
		}

		for i, q := range qs {
			q.ChunkID = c.ID

			// Run deterministic QC before accepting a draft. Semantic review is an
			// advisory set-level signal, not part of the production acceptance path.
			rep := cheap[i]
			vec := vectorsByQuestion[i]
			rep.Results = append(rep.Results, gateDistinct(q, res.Passed, vec, acceptedVecs))
			q.Report = rep
			res.Questions = append(res.Questions, q)
			passedBefore := len(res.Passed)
			keep(res, q, c, lesson, lessonName, toppedUp, vec, &acceptedVecs)
			if plan != nil && len(res.Passed) > passedBefore {
				planIndex++
			}
			d.Log.report("gate", len(res.Passed), budget, gateNote(rep))
			if !rep.Passed() {
				feedback = appendRejectedDraft(feedback, q, rep)
			}
		}
	}

	res.Ceiling = len(res.Passed) < budget
	return res, nil
}

func generateExamSet(ctx context.Context, outline *Outline, lesson Lesson, chunks []Chunk, d Deps, opt ExamOptions, budget int) (*ExamResult, error) {
	generator, ok := d.Gen.(QuestionSetGenerator)
	if !ok {
		return nil, fmt.Errorf("set generation requested but generator has no question-set method")
	}
	if outline.EvidenceGraph == nil || len(outline.EvidenceGraph.Atoms) == 0 {
		return nil, fmt.Errorf("set generation requires a compiled evidence graph with atomic claims")
	}
	contextChunks := LessonContext(lesson, outline.EvidenceGraph, chunks)
	contract := BuildCoverageContractForRun(lesson, outline.EvidenceGraph, contextChunks, budget, opt.GenerationDirective, opt.ForceCalc)
	if opt.ContractPreflight {
		before := len(contract.PreflightChanges)
		contract = PreflightCoverageContract(contract, outline.EvidenceGraph, contextChunks)
		d.Log.report("set-preflight", len(contract.PreflightChanges)-before, len(contract.Slots), "deterministic contract normalization")
	}
	if len(contract.Slots) == 0 {
		return nil, fmt.Errorf("set generation produced no coverage slots for lesson %q", lesson.Title)
	}

	d.Log.report("set-context", len(contextChunks), len(chunks), fmt.Sprintf("%d graph-derived chunks", len(contextChunks)))
	d.Log.report("set-contract", len(contract.Slots), budget, "atomic coverage slots")
	candidates := opt.SetCandidates
	if candidates <= 0 {
		candidates = 1
	}

	var best *ExamResult
	bestScore := -1
	var firstErr error
	for i := 1; i <= candidates; i++ {
		candidateContract := contract
		candidateContract.Variant = i
		qs, err := generator.QuestionsSet(ctx, lesson, outline.EvidenceGraph, contextChunks, candidateContract, nil, opt.ForceCalc)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("generate question set candidate %d: %w", i, err)
			}
			d.Log.report("set-candidate", i, candidates, fmt.Sprintf("candidate failed: %v", err))
			continue
		}
		candidate := evaluateSetCandidate(ctx, lesson, outline.EvidenceGraph, contextChunks, contract, qs, d)
		if retryContract, ok := missingCoverageContract(contract, candidate); ok {
			retryContract.Variant = i
			retryContract.GenerationDirective = joinDirectives(
				retryContract.GenerationDirective,
				"Bounded contract-repair attempt: generate only the missing slots listed below. Do not repeat questions that already passed; return no substitute slot.",
			)
			candidate.SetContractRetries = 1
			d.Log.report("set-retry", 1, maxSetContractRetries, fmt.Sprintf("%d missing slot(s)", len(retryContract.Slots)))
			retryQS, retryErr := generator.QuestionsSet(ctx, lesson, outline.EvidenceGraph, contextChunks, retryContract, rejectedDrafts(candidate.Questions), opt.ForceCalc)
			if retryErr != nil {
				d.Log.report("set-retry", 0, maxSetContractRetries, fmt.Sprintf("repair failed: %v", retryErr))
			} else {
				combined := append(append([]Question(nil), qs...), retryQS...)
				candidate = evaluateSetCandidate(ctx, lesson, outline.EvidenceGraph, contextChunks, contract, combined, d)
				candidate.SetContractRetries = 1
			}
		}
		if d.Quality != nil && len(candidate.Passed) > 0 {
			quality, qualityErr := d.Quality.GradeSet(ctx, lesson, contextChunks, candidate.Passed)
			if qualityErr != nil {
				d.Log.report("quality", 0, 0, fmt.Sprintf("advisory grader unavailable: %v", qualityErr))
			} else if quality != nil && quality.CompleteFor(len(candidate.Passed)) {
				candidate.Quality = quality
				d.Log.report("quality", quality.TotalScore, quality.MaxScore, "advisory semantic review")
			} else {
				d.Log.report("quality", 0, 0, "advisory grader returned incomplete output")
			}
		}
		score := setCandidateScore(candidate, outline.EvidenceGraph)
		d.Log.report("set-candidate", i, candidates, fmt.Sprintf("%d/%d accepted, score=%d", len(candidate.Passed), len(candidate.Questions), score))
		if best == nil || score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	if best == nil {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("set generation returned no candidate")
	}
	best.Contract = &contract
	best.SetCandidates = candidates
	best.SelectedSetScore = bestScore
	return best, nil
}

const maxSetContractRetries = 1

// missingCoverageContract creates the one bounded repair request for a
// candidate. It keeps the original contract immutable and removes slots that
// already passed, so a repair call cannot spend output tokens regenerating a
// valid question or accidentally consume an accepted slot twice.
func missingCoverageContract(contract CoverageContract, candidate *ExamResult) (CoverageContract, bool) {
	if candidate == nil || len(candidate.Passed) >= len(contract.Slots) {
		return CoverageContract{}, false
	}
	used := map[string]bool{}
	for _, q := range candidate.Passed {
		if q.CoverageSlotID != "" {
			used[q.CoverageSlotID] = true
		}
	}
	retry := contract
	retry.Slots = nil
	retry.PreflightChanges = nil
	for _, slot := range contract.Slots {
		if !used[slot.ID] {
			retry.Slots = append(retry.Slots, slot)
		}
	}
	return retry, len(retry.Slots) > 0
}

func rejectedDrafts(questions []Question) []RejectedDraft {
	var feedback []RejectedDraft
	for _, q := range questions {
		if q.Report == nil || q.Report.Passed() {
			continue
		}
		feedback = appendRejectedDraft(feedback, q, q.Report)
	}
	return feedback
}

func evaluateSetCandidate(ctx context.Context, lesson Lesson, graph *EvidenceGraph, contextChunks []Chunk, contract CoverageContract, qs []Question, d Deps) *ExamResult {
	res := &ExamResult{Lesson: lesson, Budget: contract.Budget}
	byChunk := ChunkByID(contextChunks)
	usedSlots := map[string]bool{}
	usedAtoms := map[string]bool{}
	var acceptedVecs [][]float32
	eligible := make([]Question, 0, len(qs))
	eligibleAt := make([]int, 0, len(qs))
	cheap := make([]*GateReport, len(qs))
	for i, q := range qs {
		q = RepairQuestionProvenance(q, contract, graph, contextChunks)
		qs[i] = q
		chunk := byChunk[q.EvidenceChunkID]
		q.ChunkID = q.EvidenceChunkID
		cheap[i] = RunCheapGates(q, chunk, d.Eval)
		if len(cheap[i].Failures()) == 0 && strings.TrimSpace(q.Stem) != "" {
			eligible = append(eligible, q)
			eligibleAt = append(eligibleAt, i)
		}
	}
	vectors := stemVectors(ctx, eligible, d)
	vectorsByQuestion := make([][]float32, len(qs))
	if len(vectors) == len(eligible) {
		for i, questionAt := range eligibleAt {
			vectorsByQuestion[questionAt] = vectors[i]
		}
	}

	for i, q := range qs {
		q.ChunkID = q.EvidenceChunkID
		rep := cheap[i]
		rep.Results = append(rep.Results, gateSetCoverage(q, contract, byChunk, usedSlots, usedAtoms))
		rep.Results = append(rep.Results, gateDistinct(q, res.Passed, vectorsByQuestion[i], acceptedVecs))
		q.Report = rep
		res.Questions = append(res.Questions, q)
		if rep.Passed() {
			res.Passed = append(res.Passed, q)
			acceptedVecs = append(acceptedVecs, vectorsByQuestion[i])
			usedSlots[q.CoverageSlotID] = true
			usedAtoms[q.EvidenceAtomID] = true
		}
		d.Log.report("set-gate", len(res.Passed), contract.Budget, gateNote(rep))
	}
	res.Ceiling = len(res.Passed) < contract.Budget
	return res
}

// setCandidateScore is intentionally acceptance-first. Semantic quality is a
// normalized tie-break after deterministic acceptance, then diversity prefers
// candidates that cover more atoms, skills, relations, and chunks. The grader
// is advisory: a missing or malformed report contributes nothing.
func setCandidateScore(res *ExamResult, graph *EvidenceGraph) int {
	if res == nil {
		return -1
	}
	atomRelation := map[string]string{}
	if graph != nil {
		for _, atom := range graph.Atoms {
			atomRelation[atom.ID] = strings.ToLower(strings.TrimSpace(atom.Relation))
		}
	}
	seenAtoms := map[string]bool{}
	seenSkills := map[string]bool{}
	seenRelations := map[string]bool{}
	seenChunks := map[string]bool{}
	for _, q := range res.Passed {
		seenAtoms[q.EvidenceAtomID] = true
		seenSkills[strings.ToLower(strings.TrimSpace(q.Skill))] = true
		if relation := atomRelation[q.EvidenceAtomID]; relation != "" {
			seenRelations[relation] = true
		}
		seenChunks[q.EvidenceChunkID] = true
	}
	semantic := 0
	if res.Quality != nil && res.Quality.CompleteFor(len(res.Passed)) {
		semantic = res.Quality.TotalScore * 1000 / res.Quality.MaxScore
	}
	return len(res.Passed)*1_000_000 + semantic*1_000 + len(seenAtoms)*500 + len(seenSkills)*100 + len(seenRelations)*75 + len(seenChunks)*25 - len(res.FailuresByGate())*10
}

const maxRejectedDrafts = 4

func excerpt(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func appendRejectedDraft(memory []RejectedDraft, q Question, report *GateReport) []RejectedDraft {
	choices := make([]string, len(q.Choices))
	for i, choice := range q.Choices {
		choices[i] = excerpt(choice.Content, 179)
	}
	failures := report.Failures()
	for i := range failures {
		failures[i].Reason = excerpt(failures[i].Reason, 239)
		failures[i].ChoiceVerdicts = append([]ChoiceVerdict(nil), failures[i].ChoiceVerdicts...)
		for j := range failures[i].ChoiceVerdicts {
			failures[i].ChoiceVerdicts[j].Reason = excerpt(failures[i].ChoiceVerdicts[j].Reason, 159)
		}
	}
	memory = append(memory, RejectedDraft{Stem: excerpt(q.Stem, 239), Choices: choices, Failures: failures})
	if len(memory) > maxRejectedDrafts {
		memory = append([]RejectedDraft(nil), memory[len(memory)-maxRejectedDrafts:]...)
	}
	return memory
}

func joinDirectives(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	default:
		return left + "\n\n" + right
	}
}

func questionPlanDirective(plan *QuestionPlan, current int, accepted []Question) string {
	if plan == nil || current >= len(plan.Slots) {
		return "The planned slots are exhausted. Return no question unless this passage supports a genuinely distinct additional target."
	}

	var b strings.Builder
	b.WriteString("Lesson-level question plan (shared across generation calls):\n")
	for i, slot := range plan.Slots {
		status := "upcoming"
		if i < current {
			status = "completed"
		} else if i == current {
			status = "CURRENT — generate this slot if this passage supports it"
		}
		fmt.Fprintf(&b, "%d. [%s] skill=%s difficulty=%s focus=%s target=%s", i+1, status,
			slot.Skill, slot.Difficulty, slot.Focus, slot.Target)
		if slot.Scenario != "" {
			fmt.Fprintf(&b, " scenario=%s", slot.Scenario)
		}
		if slot.SourceChunkID != "" {
			fmt.Fprintf(&b, " evidence_chunk=%s", slot.SourceChunkID)
		}
		b.WriteByte('\n')
	}
	if len(accepted) > 0 {
		b.WriteString("Already accepted targets; do not repeat them:\n")
		start := 0
		if len(accepted) > 6 {
			start = len(accepted) - 6
		}
		for i := start; i < len(accepted); i++ {
			fmt.Fprintf(&b, "- %s\n", excerpt(accepted[i].Stem, 180))
		}
	}
	b.WriteString("Generate one question for the CURRENT slot only. If the current passage does not support it, return an empty list; do not substitute a repeated target.")
	return b.String()
}

// stemVector embeds a question stem for the duplicate check. Returns nil when
// no embedder is configured or the call fails — the check then falls back to
// literal text comparison rather than blocking the run.
func stemVector(ctx context.Context, q Question, d Deps) []float32 {
	vecs := stemVectors(ctx, []Question{q}, d)
	if len(vecs) != 1 {
		return nil
	}
	return vecs[0]
}

func stemVectors(ctx context.Context, qs []Question, d Deps) [][]float32 {
	if d.Embedder == nil || len(qs) == 0 {
		return nil
	}
	texts := make([]string, len(qs))
	for i, q := range qs {
		if strings.TrimSpace(q.Stem) == "" {
			return nil
		}
		texts[i] = q.Stem
	}
	vecs, err := d.Embedder.Embed(ctx, texts)
	if err != nil || len(vecs) != len(texts) {
		return nil
	}
	return vecs
}

// keep records a question that survived every gate, and notes when it came
// from a lesson other than the one being generated for.
func keep(res *ExamResult, q Question, c Chunk, lesson Lesson, lessonName map[string]string, toppedUp map[string]bool, vec []float32, vecs *[][]float32) {
	if q.Report == nil || !q.Report.Passed() {
		return
	}
	res.Passed = append(res.Passed, q)
	*vecs = append(*vecs, vec)

	if c.LessonID == lesson.ID || toppedUp[c.LessonID] {
		return
	}
	toppedUp[c.LessonID] = true
	name := lessonName[c.LessonID]
	if name == "" {
		name = c.LessonID
	}
	res.ToppedUpFrom = append(res.ToppedUpFrom, name)
}

func gateNote(r *GateReport) string {
	if r.Passed() {
		return "passed"
	}
	f := r.Failures()
	return fmt.Sprintf("failed %s", f[0].Gate)
}

func chunksFor(ids []string, byID map[string]Chunk) []Chunk {
	out := make([]Chunk, 0, len(ids))
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	return out
}

// rankByRelevance orders chunks by cosine similarity to the query. With no
// embedder configured it returns the chunks in document order, which is a
// reasonable fallback and keeps the pipeline runnable without embeddings.
func rankByRelevance(ctx context.Context, chunks []Chunk, query string, d Deps) []Chunk {
	if d.Embedder == nil || len(chunks) < 2 || strings.TrimSpace(query) == "" {
		return chunks
	}

	texts := make([]string, 0, len(chunks)+1)
	texts = append(texts, query)
	for _, c := range chunks {
		texts = append(texts, c.Text)
	}

	vecs, err := d.Embedder.Embed(ctx, texts)
	if err != nil || len(vecs) != len(texts) {
		// Ranking is an optimisation, not a correctness requirement. Losing it
		// should not abort a run that is otherwise fine.
		d.Log.report("embed", 0, 0, fmt.Sprintf("ranking skipped: %v", err))
		return chunks
	}

	type scored struct {
		c Chunk
		s float64
	}
	list := make([]scored, len(chunks))
	for i, c := range chunks {
		list[i] = scored{c: c, s: cosine(vecs[0], vecs[i+1])}
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].s > list[b].s })

	out := make([]Chunk, len(list))
	for i, s := range list {
		out[i] = s.c
	}
	return out
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// PassRate is the headline number of the whole prototype.
func (r *ExamResult) PassRate() float64 {
	if len(r.Questions) == 0 {
		return 0
	}
	return float64(len(r.Passed)) / float64(len(r.Questions))
}

// FailuresByGate counts which gate rejected what, which is the number that
// tells you where to aim the next prompt change.
func (r *ExamResult) FailuresByGate() map[GateName]int {
	out := map[GateName]int{}
	for _, q := range r.Questions {
		if q.Report == nil {
			continue
		}
		for _, f := range q.Report.Failures() {
			out[f.Gate]++
		}
	}
	return out
}
