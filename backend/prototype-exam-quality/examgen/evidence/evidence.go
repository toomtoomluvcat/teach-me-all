package evidence

import (
	"context"
	"fmt"
	"regexp"
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
	ID                  string   `json:"id"`
	AtomID              string   `json:"atom_id"`
	SupportAtomIDs      []string `json:"support_atom_ids,omitempty"`
	SourceChunkIDs      []string `json:"source_chunk_ids"`
	ConceptIDs          []string `json:"concept_ids"`
	Skill               string   `json:"skill"`
	RequiresCalculation bool     `json:"requires_calculation"`
	Difficulty          string   `json:"difficulty"`
	Operation           string   `json:"operation"`
	Target              string   `json:"target"`
	EvidenceQuote       string   `json:"evidence_quote"`
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
	// that cannot support an explicit cognitive-skill benchmark.
	RequiredSkill string `json:"-"`
	// RequiredCalculation is an internal run target. It makes arithmetic an
	// orthogonal requirement rather than a fake skill value.
	RequiredCalculation bool `json:"-"`
}

// PreflightCoverageContract repairs only deterministic contract defects before
// the writer sees them. It never invents evidence: an invalid atom/chunk is
// dropped, a mismatched quote is replaced only by the already-validated atom
// quote, and a definition with no concrete numeric literal is not advertised
// as a numeric-required slot.
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
		validSupport := make([]string, 0, len(slot.SupportAtomIDs))
		for _, supportID := range slot.SupportAtomIDs {
			support, supportOK := atomByID[supportID]
			if !supportOK || support.ID == atom.ID {
				contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("drop support %s from %s: unknown or primary atom", supportID, slot.ID))
				continue
			}
			supportChunk, chunkOK := chunkByID[support.ChunkID]
			if !chunkOK || !contextIDs[support.ChunkID] {
				contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("drop support %s from %s: atom is outside set context", supportID, slot.ID))
				continue
			}
			validSupport = append(validSupport, support.ID)
			if !containsString(slot.SourceChunkIDs, supportChunk.ID) {
				slot.SourceChunkIDs = append(slot.SourceChunkIDs, supportChunk.ID)
			}
			seenAtoms[support.ID] = true
		}
		slot.SupportAtomIDs = validSupport
		if strings.EqualFold(slot.Skill, "application") && strings.EqualFold(slot.Difficulty, "hard") && len(slot.SupportAtomIDs) == 0 {
			contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("drop %s: hard application slot has no supporting atom", slot.ID))
			continue
		}
		if strings.TrimSpace(slot.EvidenceQuote) == "" || !strings.Contains(squeeze(chunk.Text), squeeze(slot.EvidenceQuote)) {
			slot.EvidenceQuote = atom.Quote
			contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("repair %s: use atom %s exact quote", slot.ID, atom.ID))
		}
		if strings.EqualFold(strings.TrimSpace(slot.Skill), "calculation") {
			slot.RequiresCalculation = true
		}
		slot.Skill = canonicalSkill(slot.Skill)
		if slot.RequiresCalculation && shouldDowngradeCalculation(atom) {
			if contract.RequiredCalculation {
				contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("drop %s: explicit calculation target has no concrete numeric evidence", slot.ID))
				continue
			}
			fallback := fallbackSlotSkill(atom)
			contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("downgrade %s: remove calculation flag and use %s; atom has no concrete numeric literal", slot.ID, fallback))
			slot.RequiresCalculation = false
			slot.Skill = fallback
		}
		if slot.Operation == "" {
			slot.Operation = atom.Relation
		}
		clean = append(clean, slot)
	}
	contract.Slots = clean
	return contract
}

// algebraicVariableExponentPattern matches a single-letter variable base
// raised to a power, either with an explicit operator ("b^-7", "x**3",
// "θ^-7") or as a literal Unicode superscript ("b⁻⁷") the way an extracted
// textbook page often renders it — calc.go's own evaluator already has to
// parse that same superscript alphabet for the same reason. \p{L} covers any
// Unicode letter, not just ASCII, since exponent rules commonly use Greek
// letters (θ, α) as the generic base. A claim built on this shape (an
// exponent rule stated with a generic base, not a worked number) resolves to
// a symbolic answer such as "1/b^7" rather than a decimal or integer, even
// though the exponents themselves are digits. Calculation-eligibility only
// looks for digits otherwise, so this claim would wrongly qualify without the
// extra check — the natural answer isn't numeric no matter how it's dressed.
var algebraicVariableExponentPattern = regexp.MustCompile(`\p{L}\s*(\^|\*\*)\s*-?\d|\p{L}[⁰¹²³⁴⁵⁶⁷⁸⁹⁻⁺]`)

func shouldDowngradeCalculation(atom EvidenceAtom) bool {
	if !containsConcreteNumber(atom.Claim) && !containsConcreteNumber(atom.Quote) {
		return true
	}
	if algebraicVariableExponentPattern.MatchString(atom.Claim) || algebraicVariableExponentPattern.MatchString(atom.Quote) {
		return true
	}
	return false
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

func canonicalSkill(skill string) string {
	if strings.EqualFold(strings.TrimSpace(skill), "calculation") {
		return "understanding"
	}
	return strings.ToLower(strings.TrimSpace(skill))
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

// RankContextChunks places the exact chunks selected by the coverage contract
// before incidental graph context. The set still receives the same bounded
// evidence pool, but the rune cap now spends its budget on the claims the
// writer is actually required to answer.
func RankContextChunks(chunks []Chunk, contract CoverageContract) []Chunk {
	if len(chunks) < 2 || len(contract.Slots) == 0 {
		return chunks
	}
	priority := map[string]int{}
	next := 0
	for _, slot := range contract.Slots {
		for _, id := range slot.SourceChunkIDs {
			if _, ok := priority[id]; !ok {
				priority[id] = next
				next++
			}
		}
	}
	ordered := append([]Chunk(nil), chunks...)
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, iok := priority[ordered[i].ID]
		pj, jok := priority[ordered[j].ID]
		if iok != jok {
			return iok
		}
		if iok && pi != pj {
			return pi < pj
		}
		return false
	})
	return ordered
}

// SlotLocalContextChunks narrows a broad lesson context to the exact chunks
// cited by the contract plus a small typed-neighbor fringe. The packet keeps
// the raw chunk for quote verification, while the compiled atoms carry the
// claim vocabulary. Unrelated lesson chunks are not useful generation
// context; they mainly create lost-in-the-middle competition.
func SlotLocalContextChunks(chunks []Chunk, graph *EvidenceGraph, contract CoverageContract) []Chunk {
	if graph == nil || len(contract.Slots) == 0 || len(chunks) < 2 {
		return chunks
	}
	exact := map[string]bool{}
	selectedConcepts := map[string]bool{}
	for _, slot := range contract.Slots {
		for _, chunkID := range slot.SourceChunkIDs {
			exact[chunkID] = true
		}
		for _, atomID := range append([]string{slot.AtomID}, slot.SupportAtomIDs...) {
			for _, atom := range graph.Atoms {
				if atom.ID != atomID {
					continue
				}
				for _, conceptID := range atom.ConceptIDs {
					selectedConcepts[conceptID] = true
				}
			}
		}
	}
	neighborConcepts := map[string]bool{}
	for _, edge := range graph.Edges {
		if selectedConcepts[edge.From] {
			neighborConcepts[edge.To] = true
		}
		if selectedConcepts[edge.To] {
			neighborConcepts[edge.From] = true
		}
	}
	for conceptID := range selectedConcepts {
		neighborConcepts[conceptID] = true
	}
	neighborChunks := map[string]bool{}
	for _, concept := range graph.Concepts {
		if !neighborConcepts[concept.ID] {
			continue
		}
		for _, chunkID := range concept.ChunkIDs {
			neighborChunks[chunkID] = true
		}
	}
	const maxChunks = 10
	const maxRunes = 18_000
	ordered := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if exact[chunk.ID] {
			ordered = append(ordered, chunk)
		}
	}
	for _, chunk := range chunks {
		if exact[chunk.ID] || !neighborChunks[chunk.ID] {
			continue
		}
		if len(ordered) >= maxChunks {
			break
		}
		ordered = append(ordered, chunk)
	}
	used := 0
	limited := ordered[:0]
	for _, chunk := range ordered {
		if len(limited) > 0 && used+RuneLen(chunk.Text) > maxRunes && !exact[chunk.ID] {
			continue
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
	return buildCoverageContract(lesson, graph, contextChunks, budget, "", "", false)
}

// BuildCoverageContractForRun applies explicit benchmark targets before atom
// selection. Selecting the normal mixed contract first and rewriting only the
// slot labels afterwards can leave an application slot pointing at an
// understanding atom, which the coverage gate correctly rejects.
func BuildCoverageContractForRun(lesson Lesson, graph *EvidenceGraph, contextChunks []Chunk, budget int, directive string, forceCalc bool) CoverageContract {
	skill, difficulty, requiresCalculation := coverageDirectiveTargets(directive, forceCalc)
	contract := buildCoverageContract(lesson, graph, contextChunks, budget, skill, difficulty, requiresCalculation)
	contract.GenerationDirective = directive
	contract.RequiredSkill = skill
	contract.RequiredCalculation = requiresCalculation
	return contract
}

func buildCoverageContract(lesson Lesson, graph *EvidenceGraph, contextChunks []Chunk, budget int, requiredSkill, requiredDifficulty string, requiredCalculation bool) CoverageContract {
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
	if requiredSkill == "application" && requiredDifficulty == "hard" {
		withSupport := make([]EvidenceAtom, 0, len(atoms))
		for _, atom := range atoms {
			if supportingAtomIndex(atom, atoms, nil) >= 0 {
				withSupport = append(withSupport, atom)
			}
		}
		atoms = withSupport
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
	numericAssigned := false
	preferred := []string{"understanding", "application", "recall"}
	for len(contract.Slots) < budget {
		wantedSkill := requiredSkill
		if wantedSkill == "analysis" {
			// The anchor atom is picked as an application-supporting atom;
			// analysis is an upgrade layered on top once a genuinely distinct
			// second atom to combine it with is confirmed, below.
			wantedSkill = "application"
		}
		if wantedSkill == "" {
			wantedSkill = preferred[len(contract.Slots)%len(preferred)]
		}
		picked := bestCoverageAtom(atoms, wantedSkill, requiredSkill != "", requiredCalculation, usedAtoms, usedClaims, usedRelations, usedConcepts)
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
		skill := canonicalSkill(wantedSkill)
		if skill == "" || (requiredSkill == "" && !supportsForm(atom, skill)) {
			skill = chooseNonCalculationForm(atom)
		}
		if requiredSkill == "" && usedSkills[skill] {
			for _, form := range []string{"understanding", "application", "recall"} {
				if supportsForm(atom, form) && !usedSkills[form] {
					skill = form
					break
				}
			}
		}
		var supportIDs []string
		if requiredSkill == "analysis" {
			supportIDs = analysisSupportAtomIDs(atom, atoms, usedAtoms)
			if len(supportIDs) == 0 {
				// This atom has no genuinely distinct second idea (different
				// chunk, different relation) to combine it with -- try the
				// next atom rather than emit an ungrounded analysis slot.
				continue
			}
			skill = "analysis"
		} else if requiredSkill == "" && !usedSkills["analysis"] && (skill == "application" || skill == "understanding") {
			// The rotation can land on "understanding" before "application"
			// depending on how many slots already exist; analysis is a valid
			// upgrade from either, so this checks both rather than only
			// firing when the rotation happens to pick "application" first.
			if ids := analysisSupportAtomIDs(atom, atoms, usedAtoms); len(ids) > 0 {
				skill = "analysis"
				supportIDs = ids
			}
		}
		usedSkills[skill] = true
		slotRequiresCalculation := requiredCalculation
		if !slotRequiresCalculation && requiredSkill == "" && !numericAssigned && supportsForm(atom, "calculation") {
			slotRequiresCalculation = true
			numericAssigned = true
		}
		difficulty := requiredDifficulty
		if difficulty == "" && skill != "analysis" {
			if requiredSkill == "" && !requiredCalculation {
				difficulty = "easy"
				if skill == "application" {
					difficulty = "medium"
				}
			}
		}
		// analysis is deliberately left with difficulty == "" (genuinely
		// unpinned, not defaulted to "easy") here: combining two distinct
		// source ideas doesn't by itself make a question hard -- two
		// directly-stated, closely-linked facts can be an easy analysis item,
		// same as Bloom's "analyze" is a kind of cognitive operation, not a
		// difficulty tier. Pinning the slot to any one difficulty would let
		// the coverage gate reject a truthfully-harder or truthfully-easier
		// question the writer actually produced. The writer reports the real
		// difficulty; gateDemandContract scales the reasoning_steps floor to
		// match (2 for easy/medium, 3 for hard).
		if requiredSkill == "application" && requiredDifficulty == "hard" {
			supportIDs = supportingAtomIDs(atom, atoms, usedAtoms)
		}
		if requiredSkill == "application" && requiredDifficulty == "hard" && len(supportIDs) == 0 {
			continue
		}
		for _, supportID := range supportIDs {
			usedAtoms[supportID] = true
		}
		sourceChunkIDs := []string{atom.ChunkID}
		for _, supportID := range supportIDs {
			for _, candidate := range atoms {
				if candidate.ID == supportID && !containsString(sourceChunkIDs, candidate.ChunkID) {
					sourceChunkIDs = append(sourceChunkIDs, candidate.ChunkID)
				}
			}
		}
		contract.Slots = append(contract.Slots, CoverageSlot{
			ID:                  fmt.Sprintf("S%02d", len(contract.Slots)+1),
			AtomID:              atom.ID,
			SupportAtomIDs:      supportIDs,
			SourceChunkIDs:      sourceChunkIDs,
			ConceptIDs:          append([]string(nil), atom.ConceptIDs...),
			Skill:               skill,
			RequiresCalculation: slotRequiresCalculation,
			Difficulty:          difficulty,
			Operation:           atom.Relation,
			Target:              atom.Claim,
			EvidenceQuote:       atom.Quote,
		})
	}
	return contract
}

// analysisSupportAtomIDs picks a genuinely distinct second idea to combine
// with the primary atom for an analysis-tier question. Unlike
// supportingAtomIDs (which prefers same-chunk elaboration for hard
// application), analysis requires the opposite: a different chunk and a
// different relation type, so the question needs two separate pieces of the
// source rather than one fact restated. concept_id overlap is only a soft
// bonus, never a requirement -- compiled graphs across subjects were checked
// empirically and concept_ids go unpopulated anywhere from ~40% (physics) to
// 100% (a Thai biology source) of atoms, so requiring concept overlap would
// silently starve most subjects of analysis slots.
func analysisSupportAtomIDs(primary EvidenceAtom, atoms []EvidenceAtom, used map[string]bool) []string {
	type candidate struct {
		id    string
		score int
	}
	candidates := make([]candidate, 0, len(atoms))
	for i, atom := range atoms {
		if atom.ID == primary.ID || (used != nil && used[atom.ID]) {
			continue
		}
		if atom.ChunkID == primary.ChunkID {
			continue
		}
		if strings.EqualFold(atom.Relation, primary.Relation) {
			continue
		}
		if supportsForm(atom, "calculation") {
			// Leave calculation-eligible atoms alone so the (budget-wide, one
			// per contract) numeric-slot assignment still gets a turn at
			// picking this atom as its own primary -- an analysis slot only
			// needs it as supporting evidence, not as its main subject.
			continue
		}
		score := 0
		if sharesConcept(primary.ConceptIDs, atom.ConceptIDs) {
			score += 30
		}
		if supportsForm(atom, "application") || supportsForm(atom, "understanding") {
			score += 10
		}
		candidates = append(candidates, candidate{id: atom.ID, score: score + (len(atoms) - i)})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	limit := 2
	if len(candidates) < limit {
		limit = len(candidates)
	}
	ids := make([]string, 0, limit)
	for _, c := range candidates[:limit] {
		ids = append(ids, c.id)
	}
	return ids
}

func supportingAtomIDs(primary EvidenceAtom, atoms []EvidenceAtom, used map[string]bool) []string {
	type candidate struct {
		id    string
		score int
	}
	candidates := make([]candidate, 0, len(atoms))
	for i, atom := range atoms {
		if atom.ID == primary.ID || (used != nil && used[atom.ID]) {
			continue
		}
		score := 0
		if atom.ChunkID == primary.ChunkID {
			score += 40
		}
		if sharesConcept(primary.ConceptIDs, atom.ConceptIDs) {
			score += 60
		}
		if strings.EqualFold(atom.Relation, primary.Relation) {
			score -= 20
		}
		if supportsForm(atom, "application") || supportsForm(atom, "understanding") {
			score += 10
		}
		candidates = append(candidates, candidate{id: atom.ID, score: score + (len(atoms) - i)})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	limit := 2
	if len(candidates) < limit {
		limit = len(candidates)
	}
	ids := make([]string, 0, limit)
	for _, candidate := range candidates[:limit] {
		ids = append(ids, candidate.id)
	}
	return ids
}

func supportingAtomIndex(primary EvidenceAtom, atoms []EvidenceAtom, used map[string]bool) int {
	best := -1
	bestScore := -1
	for i, candidate := range atoms {
		if candidate.ID == primary.ID || (used != nil && used[candidate.ID]) {
			continue
		}
		score := 0
		if candidate.ChunkID == primary.ChunkID {
			score += 40
		}
		if sharesConcept(primary.ConceptIDs, candidate.ConceptIDs) {
			score += 60
		}
		if strings.EqualFold(candidate.Relation, primary.Relation) {
			score -= 20
		}
		if supportsForm(candidate, "application") || supportsForm(candidate, "understanding") {
			score += 10
		}
		if score > bestScore {
			best = i
			bestScore = score
		}
	}
	return best
}

func sharesConcept(left, right []string) bool {
	seen := make(map[string]bool, len(left))
	for _, id := range left {
		seen[id] = true
	}
	for _, id := range right {
		if seen[id] {
			return true
		}
	}
	return false
}

func coverageDirectiveTargets(directive string, forceCalc bool) (skill, difficulty string, requiresCalculation bool) {
	lower := strings.ToLower(strings.TrimSpace(directive))
	switch {
	case forceCalc || strings.Contains(lower, "skill to calculation") || strings.Contains(lower, "calculation questions only") || strings.Contains(lower, "requires_calculation"):
		requiresCalculation = true
	case strings.Contains(lower, "skill to analysis") || strings.Contains(lower, "analysis questions"):
		skill = "analysis"
	case strings.Contains(lower, "skill to application") || strings.Contains(lower, "application questions"):
		skill = "application"
	case strings.Contains(lower, "skill to recall") || strings.Contains(lower, "recall questions"):
		skill = "recall"
	case strings.Contains(lower, "skill to understanding") || strings.Contains(lower, "understanding questions"):
		skill = "understanding"
	}
	switch {
	case strings.Contains(lower, "difficulty to hard") || strings.Contains(lower, "at hard level"):
		difficulty = "hard"
	case strings.Contains(lower, "difficulty to medium") || strings.Contains(lower, "at medium level"):
		difficulty = "medium"
	case strings.Contains(lower, "difficulty to easy") || strings.Contains(lower, "at easy level"):
		difficulty = "easy"
	}
	return skill, difficulty, requiresCalculation
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
	if strings.TrimSpace(q.Operation) == "" {
		q.Operation = slot.Operation
	}
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

func bestCoverageAtom(atoms []EvidenceAtom, wantedSkill string, requireForm, requireCalculation bool, usedAtoms, usedClaims, usedRelations, usedConcepts map[string]bool) int {
	best := -1
	bestScore := -1 << 30
	for i, atom := range atoms {
		if usedAtoms[atom.ID] || usedClaims[strings.ToLower(atom.Claim)] {
			continue
		}
		if requireForm && !supportsForm(atom, wantedSkill) {
			continue
		}
		if requireCalculation && !supportsForm(atom, "calculation") {
			continue
		}
		score := relationPriority(atom.Relation)
		if supportsForm(atom, wantedSkill) {
			score += 100
		}
		if requireCalculation {
			score += 80
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
			if strings.EqualFold(strings.TrimSpace(form), "calculation") && shouldDowngradeCalculation(atom) {
				continue
			}
			return true
		}
	}
	// Hosted models often under-label the usable reasoning forms while still
	// identifying the relation correctly. A causal/conditional/sequence claim
	// can support a new-scenario application question even when the optional
	// question_forms array says only recall or understanding. Keep the fallback
	// narrow; calculation still requires an explicit equation/numeric signal.
	if strings.EqualFold(strings.TrimSpace(form), "application") {
		switch strings.ToLower(strings.TrimSpace(atom.Relation)) {
		case "causal", "sequence", "condition", "comparison", "direction", "example":
			return true
		}
	}
	return false
}

func chooseForm(atom EvidenceAtom, preferred string) string {
	if supportsForm(atom, preferred) {
		return preferred
	}
	for _, form := range []string{"application", "understanding", "recall"} {
		if supportsForm(atom, form) {
			return form
		}
	}
	return preferred
}

func chooseNonCalculationForm(atom EvidenceAtom) string {
	for _, form := range []string{"application", "understanding", "recall"} {
		if supportsForm(atom, form) {
			return form
		}
	}
	return "understanding"
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
	// Only reject the direction that hides required verification: a slot that
	// demands calculation but got none. A question that volunteers a verified
	// calculation the slot didn't ask for is strictly more checked, not less,
	// so it is accepted rather than punished for being more cautious than the
	// slot required.
	if slot.RequiresCalculation && !q.NeedsCalculation() {
		res.Reason = fmt.Sprintf("slot %s requires calculation, got requires_calculation=false", slot.ID)
		return res
	}
	if slot.RequiresCalculation && q.Calculation == nil {
		res.Reason = fmt.Sprintf("numeric-required slot %s omitted calculation.expression/expected", slot.ID)
		return res
	}
	if slot.Difficulty != "" && !strings.EqualFold(strings.TrimSpace(q.Difficulty), slot.Difficulty) {
		res.Reason = fmt.Sprintf("slot %s requires difficulty %s, got %s", slot.ID, slot.Difficulty, q.Difficulty)
		return res
	}
	if slot.Skill != "" && canonicalSkill(q.Skill) != canonicalSkill(slot.Skill) {
		res.Reason = fmt.Sprintf("slot %s requires skill %s, got %s", slot.ID, slot.Skill, q.Skill)
		return res
	}
	if slot.Operation != "" && !strings.EqualFold(strings.TrimSpace(q.Operation), strings.TrimSpace(slot.Operation)) {
		res.Reason = fmt.Sprintf("slot %s requires operation %s, got %s", slot.ID, slot.Operation, q.Operation)
		return res
	}
	if strings.EqualFold(slot.Skill, "application") && (strings.EqualFold(slot.Difficulty, "medium") || strings.EqualFold(slot.Difficulty, "hard")) {
		if strings.TrimSpace(q.ChangedCondition) == "" {
			res.Reason = fmt.Sprintf("%s application slot %s must state the changed condition", slot.Difficulty, slot.ID)
			return res
		}
	}
	if strings.EqualFold(slot.Skill, "application") && strings.EqualFold(slot.Difficulty, "hard") {
		if !sameIDs(q.SupportingAtomIDs, slot.SupportAtomIDs) {
			res.Reason = fmt.Sprintf("hard application slot %s requires supporting atoms %v, got %v", slot.ID, slot.SupportAtomIDs, q.SupportingAtomIDs)
			return res
		}
		if !validReasoningSteps(q.ReasoningSteps) {
			res.Reason = fmt.Sprintf("hard application slot %s needs two distinct reasoning steps", slot.ID)
			return res
		}
	}
	if strings.EqualFold(slot.Skill, "analysis") {
		if !sameIDs(q.SupportingAtomIDs, slot.SupportAtomIDs) {
			res.Reason = fmt.Sprintf("analysis slot %s requires supporting atoms %v, got %v", slot.ID, slot.SupportAtomIDs, q.SupportingAtomIDs)
			return res
		}
		// The atom-selection step already guaranteed SourceChunkIDs spans more
		// than one chunk for an analysis slot (analysisSupportAtomIDs refuses a
		// same-chunk candidate), so this is really re-checking that the slot
		// itself was built correctly rather than trusting it blindly.
		if len(slot.SourceChunkIDs) < 2 {
			res.Reason = fmt.Sprintf("analysis slot %s does not span two distinct source chunks", slot.ID)
			return res
		}
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

func sameIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]bool, len(left))
	for _, id := range left {
		seen[id] = true
	}
	for _, id := range right {
		if !seen[id] {
			return false
		}
	}
	return true
}

func validReasoningSteps(steps []string) bool {
	if len(steps) < 2 {
		return false
	}
	seen := map[string]bool{}
	for _, step := range steps {
		key := strings.ToLower(squeeze(step))
		if len([]rune(key)) < 8 || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// GateSetCoverage is the set-level coverage check consumed by the generation
// package. The implementation stays beside the contract/evidence model so
// coverage rules do not leak into the orchestration layer.
func GateSetCoverage(q Question, contract CoverageContract, byChunk map[string]Chunk, usedSlots, usedAtoms map[string]bool) GateResult {
	return gateSetCoverage(q, contract, byChunk, usedSlots, usedAtoms)
}
