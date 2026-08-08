package evidence

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Preflight is the deterministic repair pass. It normalises a contract and a
// returned question against the source before any gate runs, and it is only
// allowed to fix defects Go can decide on its own — it never invents evidence
// on the model's behalf.

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
		// A decoy the writer cannot see is worse than none: it would leave the
		// slot demanding a spare detail with nothing named to supply it, which
		// is the state that produced invented decoys in the first place.
		if slot.DecoyAtomID != "" {
			decoy, ok := atomByID[slot.DecoyAtomID]
			switch {
			case !ok || decoy.ID == atom.ID || containsString(slot.SupportAtomIDs, decoy.ID):
				contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("drop decoy source %s from %s: unknown, primary, or already load-bearing", slot.DecoyAtomID, slot.ID))
				slot.DecoyAtomID = ""
			case !contextIDs[decoy.ChunkID]:
				contract.PreflightChanges = append(contract.PreflightChanges, fmt.Sprintf("drop decoy source %s from %s: atom is outside set context", slot.DecoyAtomID, slot.ID))
				slot.DecoyAtomID = ""
			}
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
		// The quote resolves to exactly one claim, but no open slot asks about
		// that claim — the draft answered something the contract did not
		// request. It still fails, and should, but the provenance it does have
		// is recorded so the failure is reported against the chunk the quote is
		// really in. Without this the empty chunk ID sends gateQuote looking in
		// no chunk at all, and a question that answered the wrong claim is
		// reported as having fabricated its quote, which is a different and
		// much more alarming defect than the one it has.
		q.EvidenceAtomID = matched.ID
		q.EvidenceChunkID = matched.ChunkID
		q.ChunkID = matched.ChunkID
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

// RepairDistractorAtoms strips a distractor citation that cannot be true.
//
// Measured across two subjects and four runs: the writer routinely tags a wrong
// choice with the slot's own atom, which claims the wrong choice is the answer.
// Rejecting the question for it costs a whole item over a label, and the option
// text is usually fine — the defect is in the metadata, not in the question. So
// this drops the bad citation and lets the remaining evidence decide: a slot
// that asked for close distractors now falls back to needing one distinct
// written reason per wrong option, and fails on that if the reasons are absent.
// Nothing is invented, which is the line the whole preflight pass holds.
func RepairDistractorAtoms(q Question, contract CoverageContract) Question {
	var slot *CoverageSlot
	for i := range contract.Slots {
		if contract.Slots[i].ID == q.CoverageSlotID {
			slot = &contract.Slots[i]
			break
		}
	}
	repaired := append([]Choice(nil), q.Choices...)
	changed := false
	correct := q.CorrectIndex()
	for i := range repaired {
		id := strings.TrimSpace(repaired[i].DistractorAtomID)
		if id == "" {
			continue
		}
		bogus := i == correct ||
			(slot != nil && id == slot.AtomID) ||
			(len(contract.GraphAtomIDs) > 0 && !contract.KnowsAtom(id))
		if bogus {
			repaired[i].DistractorAtomID = ""
			changed = true
		}
	}
	if changed {
		q.Choices = repaired
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
