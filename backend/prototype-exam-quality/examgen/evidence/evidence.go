package evidence

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// EvidenceCompiler is optional at the interface boundary. The lightweight
// unit-test generators do not need another model pass, while the production
// generator can compile the graph into claims before set generation.
type EvidenceCompiler interface {
	CompileEvidence(ctx context.Context, graph EvidenceGraph, chunks []Chunk) (EvidenceGraph, error)
}

// CoverageSlot is a deterministic target for one question. It is not a draft:
// it tells the writer which atom, operation, and source provenance to use.
type CoverageSlot struct {
	ID             string   `json:"id"`
	AtomID         string   `json:"atom_id"`
	SourceChunkIDs []string `json:"source_chunk_ids"`
	ConceptIDs     []string `json:"concept_ids"`
	Skill          string   `json:"skill"`
	Difficulty     string   `json:"difficulty"`
	Operation      string   `json:"operation"`
	Target         string   `json:"target"`
	EvidenceQuote  string   `json:"evidence_quote"`
}

// CoverageContract is the compact set-level contract sent to the writer.
// ContextChunkIDs includes bounded two-hop graph context, not only the selected
// lesson's chunks, so cross-lesson relationships remain available.
type CoverageContract struct {
	Budget           int            `json:"budget"`
	ContextChunkIDs  []string       `json:"context_chunk_ids"`
	Slots            []CoverageSlot `json:"slots"`
	PreflightChanges []string       `json:"preflight_changes,omitempty"`
	// GenerationDirective is an internal run instruction, not source evidence
	// or persisted contract data. It lets the set-generator carry benchmark
	// requirements without introducing a subject-specific API.
	GenerationDirective string `json:"-"`
	// Variant is an internal selector hint. It is deliberately not persisted
	// as source contract data: all candidates must satisfy the same slots.
	Variant int `json:"-"`
	// RequiredSkill is an internal run target. It lets preflight drop an atom
	// that cannot support an explicit calculation benchmark instead of silently
	// downgrading the slot to a different skill.
	RequiredSkill string `json:"-"`
}

// PreflightCoverageContract repairs only deterministic contract defects before
// the writer sees them. It never invents evidence: an invalid atom/chunk is
// dropped, a mismatched quote is replaced only by the already-validated atom
// quote, and a definition with no concrete numeric literal is not advertised
// as a calculation slot.
func PreflightCoverageContract(contract CoverageContract, graph *EvidenceGraph, chunks []Chunk) CoverageContract {
	if graph == nil {
		return contract
	}
	atomByID := map[string]EvidenceAtom{}
	for _, atom := range graph.Atoms {
		atomByID[atom.ID] = atom
	}
	chunkByID := ChunkByID(chunks)
	contextIDs := map[string]bool{}
	for _, id := range contract.ContextChunkIDs {
		contextIDs[id] = true
	}
	seenAtoms := map[string]bool{}
	clean := make([]CoverageSlot, 0, len(contract.Slots))
	for _, slot := range contract.Slots {
		atom, ok := atomByID[slot.AtomID]
		if !ok {
			contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("drop %s: unknown atom %s", slot.ID, slot.AtomID))
			continue
		}
		chunk, ok := chunkByID[atom.ChunkID]
		if !ok || !contextIDs[atom.ChunkID] {
			contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("drop %s: atom %s is outside set context", slot.ID, atom.ID))
			continue
		}
		if seenAtoms[atom.ID] {
			contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("drop %s: atom %s already used", slot.ID, atom.ID))
			continue
		}
		seenAtoms[atom.ID] = true
		slot.SourceChunkIDs = []string{atom.ChunkID}
		if strings.TrimSpace(slot.EvidenceQuote) == "" || !strings.Contains(squeeze(chunk.Text), squeeze(slot.EvidenceQuote)) {
			slot.EvidenceQuote = atom.Quote
			contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("repair %s: use atom %s exact quote", slot.ID, atom.ID))
		}
		slot.Skill = strings.ToLower(strings.TrimSpace(slot.Skill))
		if slot.Skill == "calculation" && shouldDowngradeCalculation(atom) {
			if contract.RequiredSkill == "calculation" {
				contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("drop %s: explicit calculation target has no concrete numeric evidence", slot.ID))
				continue
			}
			fallback := fallbackSlotSkill(atom)
			if fallback != slot.Skill {
				contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("downgrade %s: calculation -> %s; definition has no concrete numeric literal", slot.ID, fallback))
				slot.Skill = fallback
			}
		}
		if slot.Operation == "" {
			slot.Operation = atom.Relation
		}
		clean = append(clean, slot)
	}
	contract.Slots = clean
	return contract
}

func shouldDowngradeCalculation(atom EvidenceAtom) bool {
	if !strings.EqualFold(strings.TrimSpace(atom.Relation), "definition") {
		return false
	}
	return !containsConcreteNumber(atom.Claim) && !containsConcreteNumber(atom.Quote)
}

func containsConcreteNumber(text string) bool {
	for _, r := range text {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func fallbackSlotSkill(atom EvidenceAtom) string {
	for _, skill := range []string{"application", "understanding", "recall"} {
		if supportsForm(atom, skill) {
			return skill
		}
	}
	return "understanding"
}

// NormalizeEvidenceGraph drops compiler output with unknown provenance and
// assigns stable atom IDs. A valid-looking claim with no source chunk is not
// evidence and must not enter generation.
func NormalizeEvidenceGraph(graph EvidenceGraph, chunks []Chunk, atoms []EvidenceAtom) (EvidenceGraph, error) {
	chunkByID := ChunkByID(chunks)
	conceptByID := map[string]bool{}
	for _, concept := range graph.Concepts {
		conceptByID[concept.ID] = true
	}

	clean := make([]EvidenceAtom, 0, len(atoms))
	seen := map[string]bool{}
	for _, atom := range atoms {
		atom.Claim = strings.TrimSpace(atom.Claim)
		atom.Quote = strings.TrimSpace(atom.Quote)
		atom.Relation = strings.TrimSpace(atom.Relation)
		if atom.Claim == "" || atom.Quote == "" || atom.Relation == "" || atom.ChunkID == "" {
			continue
		}
		chunk, ok := chunkByID[atom.ChunkID]
		if !ok {
			continue
		}
		if !strings.Contains(squeeze(chunk.Text), squeeze(atom.Quote)) {
			continue
		}
		atom.Page = chunk.Page
		concepts := atom.ConceptIDs[:0]
		for _, id := range atom.ConceptIDs {
			if conceptByID[id] && !containsString(concepts, id) {
				concepts = append(concepts, id)
			}
		}
		atom.ConceptIDs = concepts
		key := atom.ChunkID + "\x00" + strings.ToLower(atom.Relation) + "\x00" + strings.ToLower(atom.Claim)
		if seen[key] {
			continue
		}
		seen[key] = true
		atom.ID = fmt.Sprintf("A%03d", len(clean)+1)
		clean = append(clean, atom)
	}
	if len(clean) == 0 {
		return graph, fmt.Errorf("evidence compiler returned no source-bound atoms")
	}
	graph.Atoms = clean
	return graph, nil
}

// LessonContext returns the lesson's chunks plus one graph hop of related
// chunks. The result is document ordered and bounded so a large book does not
// turn set generation into a document-sized prompt.
func LessonContext(lesson Lesson, graph *EvidenceGraph, chunks []Chunk) []Chunk {
	if graph == nil {
		return chunksFor(lesson.ChunkIDs, ChunkByID(chunks))
	}

	relevantConcepts := map[string]bool{}
	for _, id := range lesson.ConceptIDs {
		relevantConcepts[id] = true
	}
	for hop := 0; hop < 2; hop++ {
		before := len(relevantConcepts)
		for _, edge := range graph.Edges {
			if relevantConcepts[edge.From] {
				relevantConcepts[edge.To] = true
			}
			if relevantConcepts[edge.To] {
				relevantConcepts[edge.From] = true
			}
		}
		if len(relevantConcepts) == before {
			break
		}
	}

	ids := map[string]bool{}
	for _, id := range lesson.ChunkIDs {
		ids[id] = true
	}
	for _, concept := range graph.Concepts {
		if !relevantConcepts[concept.ID] {
			continue
		}
		for _, id := range concept.ChunkIDs {
			ids[id] = true
		}
	}
	for _, atom := range graph.Atoms {
		for _, conceptID := range atom.ConceptIDs {
			if relevantConcepts[conceptID] {
				ids[atom.ChunkID] = true
				break
			}
		}
	}

	ordered := make([]Chunk, 0, len(ids))
	for _, chunk := range chunks {
		if ids[chunk.ID] {
			ordered = append(ordered, chunk)
		}
	}
	const maxChunks = 24
	const maxRunes = 30_000
	if len(ordered) > maxChunks {
		ordered = ordered[:maxChunks]
	}
	used := 0
	limited := ordered[:0]
	for _, chunk := range ordered {
		if len(limited) > 0 && used+RuneLen(chunk.Text) > maxRunes {
			break
		}
		limited = append(limited, chunk)
		used += RuneLen(chunk.Text)
	}
	return limited
}

// BuildCoverageContract converts compiled atoms into a small, varied set
// contract without asking a second planner to invent free-text targets. It
// prefers distinct claims/relations and cycles through available reasoning
// modes before allowing a repeated mode.
func BuildCoverageContract(lesson Lesson, graph *EvidenceGraph, contextChunks []Chunk, budget int) CoverageContract {
	return buildCoverageContract(lesson, graph, contextChunks, budget, "", "")
}

// BuildCoverageContractForRun applies explicit benchmark targets before atom
// selection. Selecting the normal mixed contract first and rewriting only the
// slot labels afterwards can leave an application slot pointing at an
// understanding atom, which the coverage gate correctly rejects.
func BuildCoverageContractForRun(lesson Lesson, graph *EvidenceGraph, contextChunks []Chunk, budget int, directive string, forceCalc bool) CoverageContract {
	skill, difficulty := coverageDirectiveTargets(directive, forceCalc)
	contract := buildCoverageContract(lesson, graph, contextChunks, budget, skill, difficulty)
	contract.GenerationDirective = directive
	contract.RequiredSkill = skill
	return contract
}

func buildCoverageContract(lesson Lesson, graph *EvidenceGraph, contextChunks []Chunk, budget int, requiredSkill, requiredDifficulty string) CoverageContract {
	contract := CoverageContract{Budget: budget}
	for _, chunk := range contextChunks {
		contract.ContextChunkIDs = append(contract.ContextChunkIDs, chunk.ID)
	}
	if graph == nil || budget <= 0 {
		return contract
	}

	contextIDs := map[string]bool{}
	for _, id := range contract.ContextChunkIDs {
		contextIDs[id] = true
	}
	atoms := make([]EvidenceAtom, 0, len(graph.Atoms))
	for _, atom := range graph.Atoms {
		if !contextIDs[atom.ChunkID] {
			continue
		}
		atoms = append(atoms, atom)
	}

	sort.SliceStable(atoms, func(i, j int) bool {
		if atoms[i].ChunkID != atoms[j].ChunkID {
			return atoms[i].ChunkID < atoms[j].ChunkID
		}
		return atoms[i].ID < atoms[j].ID
	})
	usedClaims := map[string]bool{}
	usedAtoms := map[string]bool{}
	usedSkills := map[string]bool{}
	usedRelations := map[string]bool{}
	usedConcepts := map[string]bool{}
	preferred := []string{"understanding", "calculation", "application", "recall"}
	for len(contract.Slots) < budget {
		wantedSkill := requiredSkill
		if wantedSkill == "" {
			wantedSkill = preferred[len(contract.Slots)%len(preferred)]
		}
		picked := bestCoverageAtom(atoms, wantedSkill, requiredSkill != "", usedAtoms, usedClaims, usedRelations, usedConcepts)
		if picked < 0 {
			break
		}
		atom := atoms[picked]
		usedAtoms[atom.ID] = true
		usedClaims[strings.ToLower(atom.Claim)] = true
		usedRelations[strings.ToLower(atom.Relation)] = true
		for _, conceptID := range atom.ConceptIDs {
			usedConcepts[conceptID] = true
		}
		skill := wantedSkill
		if skill == "" {
			skill = chooseForm(atom, wantedSkill)
		}
		if requiredSkill == "" && usedSkills[skill] {
			for _, form := range []string{"understanding", "calculation", "application", "recall"} {
				if supportsForm(atom, form) && !usedSkills[form] {
					skill = form
					break
				}
			}
		}
		usedSkills[skill] = true
		difficulty := requiredDifficulty
		if difficulty == "" {
			if requiredSkill == "" {
				difficulty = "easy"
				if skill == "application" || skill == "calculation" {
					difficulty = "medium"
				}
			}
		}
		contract.Slots = append(contract.Slots, CoverageSlot{
			ID:             fmt.Sprintf("S%02d", len(contract.Slots)+1),
			AtomID:         atom.ID,
			SourceChunkIDs: []string{atom.ChunkID},
			ConceptIDs:     append([]string(nil), atom.ConceptIDs...),
			Skill:          skill,
			Difficulty:     difficulty,
			Operation:      atom.Relation,
			Target:         atom.Claim,
			EvidenceQuote:  atom.Quote,
		})
	}
	return contract
}

func coverageDirectiveTargets(directive string, forceCalc bool) (skill, difficulty string) {
	lower := strings.ToLower(strings.TrimSpace(directive))
	switch {
	case forceCalc || strings.Contains(lower, "skill to calculation") || strings.Contains(lower, "calculation questions only"):
		skill = "calculation"
	case strings.Contains(lower, "skill to application") || strings.Contains(lower, "application questions"):
		skill = "application"
	}
	switch {
	case strings.Contains(lower, "difficulty to hard") || strings.Contains(lower, "at hard level"):
		difficulty = "hard"
	case strings.Contains(lower, "difficulty to easy") || strings.Contains(lower, "at easy level"):
		difficulty = "easy"
	}
	return skill, difficulty
}

// RepairQuestionProvenance fills omitted or inconsistent set provenance only
// when the question's exact source_quote uniquely identifies one graph atom
// and that atom is the atom assigned to a compatible coverage slot. It is a
// deterministic recovery for provider JSON omissions, not a relaxation of
// the coverage contract: ambiguous or unsupported quotes remain rejected.
func RepairQuestionProvenance(q Question, contract CoverageContract, graph *EvidenceGraph, chunks []Chunk) Question {
	if graph == nil || strings.TrimSpace(q.SourceQuote) == "" {
		return q
	}
	byChunk := ChunkByID(chunks)
	var matched EvidenceAtom
	matches := 0
	for _, atom := range graph.Atoms {
		chunk, ok := byChunk[atom.ChunkID]
		if !ok || !quoteMatchesChunk(q.SourceQuote, chunk) || !quoteMatches(q.SourceQuote, atom.Quote) {
			continue
		}
		matched = atom
		matches++
	}
	if matches != 1 {
		return q
	}

	var slot *CoverageSlot
	for i := range contract.Slots {
		candidate := &contract.Slots[i]
		if candidate.AtomID != matched.ID || !containsString(candidate.SourceChunkIDs, matched.ChunkID) {
			continue
		}
		if q.CoverageSlotID != "" && q.CoverageSlotID != candidate.ID {
			continue
		}
		if candidate.Skill != "" && q.Skill != "" && !strings.EqualFold(candidate.Skill, q.Skill) {
			continue
		}
		if candidate.Difficulty != "" && q.Difficulty != "" && !strings.EqualFold(candidate.Difficulty, q.Difficulty) {
			continue
		}
		if slot != nil {
			return q
		}
		slot = candidate
	}
	if slot == nil {
		return q
	}
	q.CoverageSlotID = slot.ID
	q.EvidenceAtomID = matched.ID
	q.EvidenceChunkID = matched.ChunkID
	q.ChunkID = matched.ChunkID
	return q
}

func quoteMatchesChunk(quote string, chunk Chunk) bool {
	return strings.Contains(squeeze(chunk.Text), squeeze(quote))
}

func quoteMatches(left, right string) bool {
	left = squeeze(stripQuoteMarks(left))
	right = squeeze(stripQuoteMarks(right))
	return left != "" && right != "" && (strings.Contains(left, right) || strings.Contains(right, left))
}

func squeeze(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func stripQuoteMarks(s string) string {
	return strings.Trim(s, " \t\r\n\"'“”‘’`")
}

func bestCoverageAtom(atoms []EvidenceAtom, wantedSkill string, requireForm bool, usedAtoms, usedClaims, usedRelations, usedConcepts map[string]bool) int {
	best := -1
	bestScore := -1 << 30
	for i, atom := range atoms {
		if usedAtoms[atom.ID] || usedClaims[strings.ToLower(atom.Claim)] {
			continue
		}
		if requireForm && !supportsForm(atom, wantedSkill) {
			continue
		}
		score := relationPriority(atom.Relation)
		if supportsForm(atom, wantedSkill) {
			score += 100
		}
		if !usedRelations[strings.ToLower(atom.Relation)] {
			score += 55
		} else {
			score -= 25
		}
		newConcept := false
		for _, conceptID := range atom.ConceptIDs {
			if !usedConcepts[conceptID] {
				newConcept = true
				break
			}
		}
		if newConcept {
			score += 20
		}
		// Stable document order is the tie-breaker. It keeps the contract
		// reproducible while the relation/skill scores provide set variety.
		if score > bestScore {
			best = i
			bestScore = score
		}
	}
	return best
}

func relationPriority(relation string) int {
	switch strings.ToLower(strings.TrimSpace(relation)) {
	case "direction":
		return 90
	case "causal":
		return 85
	case "comparison":
		return 80
	case "condition":
		return 75
	case "sequence":
		return 70
	case "equation":
		return 65
	case "example":
		return 60
	case "observation":
		return 55
	case "definition":
		return 45
	default:
		return 40
	}
}

func supportsForm(atom EvidenceAtom, form string) bool {
	for _, candidate := range atom.QuestionForms {
		if strings.EqualFold(strings.TrimSpace(candidate), form) {
			return true
		}
	}
	return false
}

func chooseForm(atom EvidenceAtom, preferred string) string {
	if supportsForm(atom, preferred) {
		return preferred
	}
	for _, form := range []string{"understanding", "calculation", "application", "recall"} {
		if supportsForm(atom, form) {
			return form
		}
	}
	return preferred
}

func gateSetCoverage(q Question, contract CoverageContract, byChunk map[string]Chunk, usedSlots, usedAtoms map[string]bool) GateResult {
	res := GateResult{Gate: GateCoverage, Deterministic: true}
	var slot *CoverageSlot
	for i := range contract.Slots {
		if contract.Slots[i].ID == q.CoverageSlotID {
			slot = &contract.Slots[i]
			break
		}
	}
	if slot == nil {
		res.Reason = fmt.Sprintf("unknown coverage slot %q", q.CoverageSlotID)
		return res
	}
	if usedSlots[q.CoverageSlotID] {
		res.Reason = fmt.Sprintf("coverage slot %s was already used", q.CoverageSlotID)
		return res
	}
	if q.EvidenceAtomID == "" || q.EvidenceAtomID != slot.AtomID {
		res.Reason = fmt.Sprintf("slot %s requires evidence atom %s, got %q", slot.ID, slot.AtomID, q.EvidenceAtomID)
		return res
	}
	if usedAtoms[q.EvidenceAtomID] {
		res.Reason = fmt.Sprintf("evidence atom %s was already used", q.EvidenceAtomID)
		return res
	}
	if !containsString(slot.SourceChunkIDs, q.EvidenceChunkID) {
		res.Reason = fmt.Sprintf("atom %s is not supported by cited chunk %s", q.EvidenceAtomID, q.EvidenceChunkID)
		return res
	}
	if _, ok := byChunk[q.EvidenceChunkID]; !ok {
		res.Reason = fmt.Sprintf("cited evidence chunk %s is not in the set context", q.EvidenceChunkID)
		return res
	}
	if slot.Skill == "calculation" && q.Calculation == nil {
		res.Reason = fmt.Sprintf("calculation slot %s omitted calculation.expression/expected", slot.ID)
		return res
	}
	if slot.Difficulty != "" && !strings.EqualFold(strings.TrimSpace(q.Difficulty), slot.Difficulty) {
		res.Reason = fmt.Sprintf("slot %s requires difficulty %s, got %s", slot.ID, slot.Difficulty, q.Difficulty)
		return res
	}
	if slot.Skill != "" && !strings.EqualFold(strings.TrimSpace(q.Skill), slot.Skill) {
		res.Reason = fmt.Sprintf("slot %s requires skill %s, got %s", slot.ID, slot.Skill, q.Skill)
		return res
	}
	if slot.EvidenceQuote != "" {
		quote := squeeze(q.SourceQuote)
		atomQuote := squeeze(slot.EvidenceQuote)
		if !strings.Contains(quote, atomQuote) && !strings.Contains(atomQuote, quote) {
			res.Reason = fmt.Sprintf("source quote does not cover the atom evidence for slot %s", slot.ID)
			return res
		}
	}
	res.Pass = true
	res.Reason = fmt.Sprintf("slot %s uses atom %s from chunk %s", slot.ID, q.EvidenceAtomID, q.EvidenceChunkID)
	return res
}

// GateSetCoverage is the set-level coverage check consumed by the generation
// package. The implementation stays beside the contract/evidence model so
// coverage rules do not leak into the orchestration layer.
func GateSetCoverage(q Question, contract CoverageContract, byChunk map[string]Chunk, usedSlots, usedAtoms map[string]bool) GateResult {
	return gateSetCoverage(q, contract, byChunk, usedSlots, usedAtoms)
}
