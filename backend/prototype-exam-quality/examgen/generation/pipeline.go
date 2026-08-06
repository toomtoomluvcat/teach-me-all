package generation

import (
	"context"
	"fmt"
	"strings"
)

// Generator is the model-backed half of generation. Advisory quality review is
// kept outside the acceptance loop so deterministic QC stays cheap and clear.
type Generator interface {
	Topics(ctx context.Context, c Chunk) (ChunkTopics, error)
	// CompileEvidence splits the classified source into atomic claims. Set
	// generation writes against those atoms, so this is not optional.
	CompileEvidence(ctx context.Context, graph EvidenceGraph, chunks []Chunk) (EvidenceGraph, error)
	Outline(ctx context.Context, graph EvidenceGraph) (*Outline, []LessonConcepts, error)
	// QuestionsSet receives the whole lesson context and a validated coverage
	// contract in one call, so the writer can keep target variety across the
	// set instead of being reset once per chunk.
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
	// ForceCalc requires arithmetic on every generated question. It does not
	// change the cognitive Skill label.
	ForceCalc bool
	// GenerationDirective is an optional benchmark-only instruction appended to
	// the question prompt. Normal production generation leaves it empty.
	GenerationDirective string
	// SetCandidates asks the set-capable generator for independent candidate
	// sets. The deterministic selector keeps the best accepted/diverse set.
	SetCandidates int
	// ContractPreflight repairs/drops deterministic slot defects before asking
	// the model to write the set. It costs no model call.
	ContractPreflight bool
	// StopOnFullSet stops generating candidates as soon as one covers every
	// contract slot. Acceptance dominates candidate selection, so such a set is
	// already unbeatable on that axis; what the remaining candidates could still
	// add is variety, which is why this is off by default.
	StopOnFullSet bool
}

// DefaultExamOptions is sized so a run finishes in minutes, not hours.
func DefaultExamOptions() ExamOptions {
	return ExamOptions{SetCandidates: 1, ContractPreflight: true}
}

// ExamResult is everything one generation run produced, kept whole. Questions
// that failed a gate are in Questions with a report explaining why; they are
// simply absent from Passed.
type ExamResult struct {
	Lesson             Lesson
	Budget             int
	Contract           *CoverageContract `json:"contract,omitempty"`
	Questions          []Question
	Passed             []Question
	SetCandidates      int `json:"set_candidates,omitempty"`
	SetContractRetries int `json:"set_contract_retries,omitempty"`
	SelectedSetScore   int `json:"selected_set_score,omitempty"`
	// Quality is advisory semantic review for the selected set. It is not a
	// hard acceptance gate and may be nil when the grader was unavailable.
	Quality *QualityReport `json:"quality,omitempty"`
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
	if opt.SetCandidates <= 0 {
		opt = DefaultExamOptions()
	}
	budget := lesson.QuestionBudget
	if opt.Budget > 0 {
		budget = opt.Budget
	}
	if budget <= 0 {
		budget = 5
	}
	return generateExamSet(ctx, outline, lesson, chunks, d, opt, budget)
}

func generateExamSet(ctx context.Context, outline *Outline, lesson Lesson, chunks []Chunk, d Deps, opt ExamOptions, budget int) (*ExamResult, error) {
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
	localContext := SlotLocalContextChunks(contextChunks, outline.EvidenceGraph, contract)
	if len(localContext) < len(contextChunks) {
		d.Log.report("set-context", len(localContext), len(contextChunks), "slot-local evidence packet with typed neighbors")
	}
	contextChunks = localContext
	contextChunks = RankContextChunks(contextChunks, contract)
	if len(contract.Slots) == 0 {
		return nil, fmt.Errorf("set generation produced no coverage slots for lesson %q", lesson.Title)
	}

	d.Log.report("set-context", len(contextChunks), len(chunks), fmt.Sprintf("%d graph-derived chunks", len(contextChunks)))
	d.Log.report("set-contract", len(contract.Slots), budget, "atomic coverage slots")
	candidates := opt.SetCandidates
	if candidates <= 0 {
		candidates = 1
	}

	var drafted []*ExamResult
	var firstErr error
	for i := 1; i <= candidates; i++ {
		candidateContract := contract
		candidateContract.Variant = i
		qs, err := d.Gen.QuestionsSet(ctx, lesson, outline.EvidenceGraph, contextChunks, candidateContract, nil, opt.ForceCalc)
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
			retryQS, retryErr := d.Gen.QuestionsSet(ctx, lesson, outline.EvidenceGraph, contextChunks, retryContract, rejectedDrafts(candidate.Questions), opt.ForceCalc)
			if retryErr != nil {
				d.Log.report("set-retry", 0, maxSetContractRetries, fmt.Sprintf("repair failed: %v", retryErr))
			} else {
				combined := append(append([]Question(nil), qs...), retryQS...)
				candidate = evaluateSetCandidate(ctx, lesson, outline.EvidenceGraph, contextChunks, contract, combined, d)
				candidate.SetContractRetries = 1
			}
		}
		drafted = append(drafted, candidate)
		d.Log.report("set-candidate", i, candidates, fmt.Sprintf("%d/%d accepted", len(candidate.Passed), len(candidate.Questions)))

		// A candidate that filled every slot cannot be beaten on acceptance, and
		// acceptance dominates the score. Generating the remaining candidates can
		// still find a more varied set, so this is opt-in rather than the default.
		if opt.StopOnFullSet && len(candidate.Passed) >= len(contract.Slots) {
			d.Log.report("set-candidate", i, candidates, "contract fully covered; skipping the remaining candidates")
			break
		}
	}
	if len(drafted) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("set generation returned no candidate")
	}

	best := selectSetCandidate(ctx, lesson, outline.EvidenceGraph, contextChunks, drafted, d)
	best.Contract = &contract
	// len(drafted), not the requested count: --stop-on-full-set and a failed
	// candidate both make those differ, and the artifact should say what the run
	// actually generated.
	best.SetCandidates = len(drafted)
	best.SelectedSetScore = setCandidateScore(best, outline.EvidenceGraph)
	return best, nil
}

// selectSetCandidate picks the set to ship and grades only the candidates that
// can still win.
//
// Semantic review is a tie-break below acceptance, so a candidate that already
// accepted fewer slots than another cannot overtake it no matter how it grades.
// Grading it anyway is one full review call spent on an answer that is already
// decided, once per losing candidate. The winner is still graded, so the run
// report carries the same advisory review it always did.
func selectSetCandidate(ctx context.Context, lesson Lesson, graph *EvidenceGraph, contextChunks []Chunk, drafted []*ExamResult, d Deps) *ExamResult {
	contenders := drafted[:1]
	for _, candidate := range drafted[1:] {
		switch {
		case len(candidate.Passed) > len(contenders[0].Passed):
			contenders = []*ExamResult{candidate}
		case len(candidate.Passed) == len(contenders[0].Passed):
			contenders = append(contenders, candidate)
		}
	}
	if len(contenders) > 1 {
		d.Log.report("quality", len(contenders), len(drafted), "grading only the candidates tied on acceptance")
	}

	best := contenders[0]
	bestScore := -1
	for _, candidate := range contenders {
		gradeSetCandidate(ctx, lesson, contextChunks, candidate, d)
		if score := setCandidateScore(candidate, graph); score > bestScore {
			best, bestScore = candidate, score
		}
	}
	return best
}

func gradeSetCandidate(ctx context.Context, lesson Lesson, contextChunks []Chunk, candidate *ExamResult, d Deps) {
	if d.Quality == nil || len(candidate.Passed) == 0 {
		return
	}
	quality, err := d.Quality.GradeSet(ctx, lesson, contextChunks, candidate.Passed)
	switch {
	case err != nil:
		d.Log.report("quality", 0, 0, fmt.Sprintf("advisory grader unavailable: %v", err))
	case quality != nil && quality.CompleteFor(len(candidate.Passed)):
		candidate.Quality = quality
		d.Log.report("quality", quality.TotalScore, quality.MaxScore, "advisory semantic review")
	default:
		d.Log.report("quality", 0, 0, "advisory grader returned incomplete output")
	}
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
	// The acceptance weight is an order of magnitude above the semantic band on
	// purpose. semantic maxes out at 1000, so at the old 1_000_000 weight a
	// perfectly-graded set could tie a set that accepted one more question —
	// which contradicts "acceptance-first" and would make the selector's answer
	// depend on which candidates happened to be graded.
	return len(res.Passed)*10_000_000 + semantic*1_000 + len(seenAtoms)*500 + len(seenSkills)*100 + len(seenRelations)*75 + len(seenChunks)*25 - len(res.FailuresByGate())*10
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

func gateNote(r *GateReport) string {
	if r.Passed() {
		return "passed"
	}
	f := r.Failures()
	return fmt.Sprintf("failed %s", f[0].Gate)
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
