package evidence

import (
	"fmt"
	"sort"
	"strings"
)

// Coverage contracts turn the evidence graph into one slot per question:
// which atom, which skill and difficulty, and whether arithmetic is required.
// Selection lives here too, because which atom a slot gets and what the slot
// may ask of it are the same decision.

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

func chooseNonCalculationForm(atom EvidenceAtom) string {
	for _, form := range []string{"application", "understanding", "recall"} {
		if supportsForm(atom, form) {
			return form
		}
	}
	return "understanding"
}
