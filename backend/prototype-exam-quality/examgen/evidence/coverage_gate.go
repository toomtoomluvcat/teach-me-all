package evidence

import (
	"fmt"
	"strings"
)

// The coverage gate is the contract's enforcement half: it checks a written
// question against the slot it claims, including provenance, declared demand,
// and the calculation flag.

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
		if !coversAll(q.SupportingAtomIDs, slot.SupportAtomIDs) {
			res.Reason = fmt.Sprintf("hard application slot %s requires supporting atoms %v, got %v", slot.ID, slot.SupportAtomIDs, q.SupportingAtomIDs)
			return res
		}
		if !validReasoningSteps(q.ReasoningSteps) {
			res.Reason = fmt.Sprintf("hard application slot %s needs two distinct reasoning steps", slot.ID)
			return res
		}
	}
	if strings.EqualFold(slot.Skill, "analysis") {
		if !coversAll(q.SupportingAtomIDs, slot.SupportAtomIDs) {
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

// coversAll reports whether have contains every id in required. It deliberately
// allows extras: a draft that volunteers an additional supporting atom is still
// fully checkable (every required atom is present), so rejecting it punishes
// the draft for being more cautious than the slot asked. Only the direction
// that hides required evidence — a missing required atom — is rejected.
func coversAll(have, required []string) bool {
	if len(have) < len(required) {
		return false
	}
	seen := make(map[string]bool, len(have))
	for _, id := range have {
		seen[id] = true
	}
	for _, id := range required {
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
