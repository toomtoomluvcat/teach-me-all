package examgen

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// Generator is the model-backed half of generation. Kept separate from Judge so
// a future version can point them at two different models without touching this
// file.
type Generator interface {
	Topics(ctx context.Context, c Chunk) ([]string, error)
	Outline(ctx context.Context, topics []string) (*Outline, []LessonTopics, error)
	Questions(ctx context.Context, lessonTitle string, c Chunk, want int, forceCalc bool) ([]Question, error)
	// Repair takes one rejected question back to the model with the exact
	// objection. Returns ok=false when the model declined to salvage it.
	Repair(ctx context.Context, q Question, c Chunk, failures []GateResult, forceCalc bool) (Question, bool, error)
}

// LessonTopics is the raw topic membership the reduce step returned, before we
// resolve it back to chunks.
type LessonTopics struct {
	LessonID string
	Topics   []string
}

// Embedder turns text into vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
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
	Gen      Generator
	Judge    Judge
	Eval     Evaluator
	Embedder Embedder
	Log      Progress
	// Parallel is how many model calls may be in flight at once. Ollama serves
	// concurrent requests from one loaded model when OLLAMA_NUM_PARALLEL allows
	// it, and the calls this pipeline makes are independent of each other, so
	// the wall clock drops without changing a single verdict. 1 disables it.
	Parallel int
}

func (d Deps) slots() int {
	if d.Parallel < 1 {
		return 1
	}
	return d.Parallel
}

// nonContent is the sentinel the map-step prompt asks for on front matter,
// tables of contents and page furniture.
const nonContent = "NON_CONTENT"

// BuildOutline runs pass 1: name the topics in every chunk, then fold those
// topics into lessons, then resolve lesson membership back to chunk IDs.
//
// Chunks are returned with LessonID filled in. Chunks whose only topic was
// NON_CONTENT come back with an empty LessonID and are never used for questions.
func BuildOutline(ctx context.Context, chunks []Chunk, d Deps) (*Outline, []Chunk, error) {
	if d.Gen == nil {
		return nil, nil, fmt.Errorf("no generator configured")
	}

	// map: chunk -> topics, remembering which chunks produced each topic.
	//
	// Every chunk is independent, so this is the easiest place in the pipeline
	// to run concurrently. Results are collected by index and merged in document
	// order afterwards, because lesson ordering depends on it.
	perChunk := make([][]string, len(chunks))
	var (
		wg       sync.WaitGroup
		sem      = make(chan struct{}, d.slots())
		mu       sync.Mutex
		done     int
		firstErr error
	)
	for i, c := range chunks {
		wg.Add(1)
		go func(i int, c Chunk) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			topics, err := d.Gen.Topics(ctx, c)

			mu.Lock()
			defer mu.Unlock()
			done++
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("topics for chunk %s: %w", c.ID, err)
				}
				return
			}
			perChunk[i] = topics
			d.Log.report("outline/map", done, len(chunks), fmt.Sprintf("page %d", c.Page))
		}(i, c)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, nil, firstErr
	}

	owners := map[string][]string{} // topic -> chunk IDs
	var order []string              // topics in first-seen order

	for i, c := range chunks {
		for _, t := range perChunk[i] {
			t = strings.TrimSpace(t)
			if t == "" || strings.EqualFold(t, nonContent) {
				continue
			}
			if _, seen := owners[t]; !seen {
				order = append(order, t)
			}
			owners[t] = append(owners[t], c.ID)
		}
	}
	if len(order) == 0 {
		return nil, nil, fmt.Errorf("pass 1 found no teaching content in %d chunks — check the extracted text before blaming the model", len(chunks))
	}

	// reduce: topics -> lessons.
	d.Log.report("outline/reduce", 0, 1, fmt.Sprintf("%d topics", len(order)))
	outline, membership, err := d.Gen.Outline(ctx, order)
	if err != nil {
		return nil, nil, fmt.Errorf("outline reduce: %w", err)
	}
	d.Log.report("outline/reduce", 1, 1, fmt.Sprintf("%d lessons", len(outline.Lessons)))

	// resolve: lesson -> chunk IDs, via the topics each lesson claimed.
	// The model is told to copy topic titles verbatim; when it does not, the
	// topic is matched loosely rather than silently dropped.
	assigned := map[string]string{} // chunk ID -> lesson ID
	byID := map[string]*Lesson{}
	for i := range outline.Lessons {
		byID[outline.Lessons[i].ID] = &outline.Lessons[i]
	}
	for _, m := range membership {
		lesson := byID[m.LessonID]
		if lesson == nil {
			continue
		}
		for _, t := range m.Topics {
			ids := owners[t]
			if ids == nil {
				ids = owners[looseMatch(t, order)]
			}
			for _, id := range ids {
				if _, taken := assigned[id]; taken {
					continue
				}
				assigned[id] = lesson.ID
				lesson.ChunkIDs = append(lesson.ChunkIDs, id)
			}
		}
	}

	out := make([]Chunk, len(chunks))
	copy(out, chunks)
	for i := range out {
		out[i].LessonID = assigned[out[i].ID]
	}

	// A lesson with no chunks cannot produce grounded questions; drop it rather
	// than let it sit in the picker as a trap.
	kept := outline.Lessons[:0]
	for _, l := range outline.Lessons {
		if len(l.ChunkIDs) > 0 {
			kept = append(kept, l)
		}
	}
	outline.Lessons = kept
	if len(outline.Lessons) == 0 {
		return nil, nil, fmt.Errorf("every lesson came back with no chunks attached — the reduce step did not copy topic titles verbatim")
	}

	return outline, out, nil
}

// looseMatch finds the closest known topic to one the model reworded. Returns
// "" when nothing is close enough, which leaves the topic unresolved rather
// than attaching a lesson to the wrong material.
func looseMatch(want string, known []string) string {
	w := strings.ToLower(strings.TrimSpace(want))
	for _, k := range known {
		lk := strings.ToLower(k)
		if lk == w || strings.Contains(lk, w) || strings.Contains(w, lk) {
			return k
		}
	}
	return ""
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
	// PerChunk is how many questions to ask for from one chunk at a time.
	PerChunk int
	// MaxChunkVisits caps total work so a lesson that cannot fill its budget
	// stops instead of grinding through the whole book.
	MaxChunkVisits int
	// Repair sends questions rejected by a deterministic gate back to the model
	// once, with the exact discrepancy.
	//
	// Off by default, and that is a measurement, not a guess. On
	// typhoon2.5-qwen3-4b it was tried on two documents: 4 questions sent back,
	// 0 came back clean, on both. Telling a 4B model its own arithmetic
	// disagrees with an independent evaluation does not get the arithmetic
	// fixed — it costs a model call per rejected question and returns nothing.
	// Worth re-measuring on a frontier model, where the answer may invert.
	Repair bool
}

// DefaultExamOptions is sized so a run finishes in minutes, not hours.
func DefaultExamOptions() ExamOptions {
	return ExamOptions{PerChunk: 2, MaxChunkVisits: 24, Repair: false}
}

// ExamResult is everything one generation run produced, kept whole. Questions
// that failed a gate are in Questions with a report explaining why; they are
// simply absent from Passed.
type ExamResult struct {
	Lesson      Lesson
	Budget      int
	Questions   []Question
	Passed      []Question
	ChunkVisits int
	// RepairAttempts and RepairsAccepted measure whether the retry loop earns
	// its cost. If accepted stays near zero, drop the loop.
	RepairAttempts  int
	RepairsAccepted int
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

	for _, c := range pool {
		if len(res.Passed) >= budget || res.ChunkVisits >= opt.MaxChunkVisits {
			break
		}
		res.ChunkVisits++
		want := budget - len(res.Passed)
		if want > opt.PerChunk {
			want = opt.PerChunk
		}

		d.Log.report("generate", len(res.Passed), budget, fmt.Sprintf("page %d", c.Page))

		qs, err := d.Gen.Questions(ctx, lesson.Title, c, want, opt.ForceCalc)
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

			// Cheap checks and the duplicate check first, judges last. Two judge
			// calls cost seconds. On a measured run the duplicate check alone
			// rejected 7 of 16, and every one of those had already been through
			// both judges.
			rep := cheap[i]
			vec := vectorsByQuestion[i]
			rep.add(gateDistinct(q, res.Passed, vec, acceptedVecs))
			if err := AddJudgeGates(ctx, rep, q, c, d.Judge); err != nil {
				return nil, err
			}
			q.Report = rep
			res.Questions = append(res.Questions, q)
			keep(res, q, c, lesson, lessonName, toppedUp, vec, &acceptedVecs)
			d.Log.report("gate", len(res.Passed), budget, gateNote(rep))

			// A question rejected only by Go — a misquote or arithmetic that
			// does not add up — gets one chance to be corrected with the exact
			// discrepancy in hand. Measured on a 4B model, four out of five
			// calculation questions had arithmetic the model itself got wrong;
			// throwing all of them away and generating from scratch costs far
			// more than one targeted retry.
			if rep.Passed() || !rep.Repairable() || !opt.Repair {
				continue
			}
			res.RepairAttempts++
			fixed, ok, err := d.Gen.Repair(ctx, q, c, rep.Failures(), opt.ForceCalc)
			if err != nil {
				return nil, fmt.Errorf("repair from chunk %s: %w", c.ID, err)
			}
			if !ok {
				continue
			}
			fixed.ChunkID = c.ID
			fixed.Attempt = q.Attempt + 1
			frep := RunCheapGates(fixed, c, d.Eval)
			fvec := stemVector(ctx, fixed, d)
			frep.add(gateDistinct(fixed, res.Passed, fvec, acceptedVecs))
			if err := AddJudgeGates(ctx, frep, fixed, c, d.Judge); err != nil {
				return nil, err
			}
			fixed.Report = frep
			res.Questions = append(res.Questions, fixed)
			if frep.Passed() {
				res.RepairsAccepted++
			}
			keep(res, fixed, c, lesson, lessonName, toppedUp, fvec, &acceptedVecs)
			d.Log.report("repair", len(res.Passed), budget, gateNote(frep))
		}
	}

	res.Ceiling = len(res.Passed) < budget
	return res, nil
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
