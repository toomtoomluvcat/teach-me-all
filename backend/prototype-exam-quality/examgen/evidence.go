package examgen

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// EvidenceAtom is the smallest teachable claim that the compiler can safely
// point back to. Topics are useful for navigation; atoms are what a question
// must depend on. The prose fields stay source-bound and the IDs are assigned
// by Go after the model returns, so a model cannot invent provenance.
type EvidenceAtom struct {
	ID            string   `json:"id"`
	ChunkID       string   `json:"chunk_id"`
	Page          int      `json:"page"`
	ConceptIDs    []string `json:"concept_ids"`
	Claim         string   `json:"claim"`
	Quote         string   `json:"evidence_quote"`
	Relation      string   `json:"relation"`
	Conditions    []string `json:"conditions"`
	Variables     []string `json:"variables"`
	QuestionForms []string `json:"question_forms"`
}

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
	Budget          int            `json:"budget"`
	ContextChunkIDs []string       `json:"context_chunk_ids"`
	Slots           []CoverageSlot `json:"slots"`
	// Variant is an internal selector hint. It is deliberately not persisted
	// as source contract data: all candidates must satisfy the same slots.
	Variant int `json:"-"`
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
		wantedSkill := preferred[len(contract.Slots)%len(preferred)]
		picked := bestCoverageAtom(atoms, wantedSkill, usedAtoms, usedClaims, usedRelations, usedConcepts)
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
		skill := chooseForm(atom, wantedSkill)
		if usedSkills[skill] {
			for _, form := range []string{"understanding", "calculation", "application", "recall"} {
				if supportsForm(atom, form) && !usedSkills[form] {
					skill = form
					break
				}
			}
		}
		usedSkills[skill] = true
		difficulty := "easy"
		if skill == "application" || skill == "calculation" {
			difficulty = "medium"
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

func bestCoverageAtom(atoms []EvidenceAtom, wantedSkill string, usedAtoms, usedClaims, usedRelations, usedConcepts map[string]bool) int {
	best := -1
	bestScore := -1 << 30
	for i, atom := range atoms {
		if usedAtoms[atom.ID] || usedClaims[strings.ToLower(atom.Claim)] {
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
