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
	// The three difficulty axes. Each is checked against something the question
	// carries, never against the difficulty word it reported: depth against the
	// claims it cites, noise against verified decoys, discrimination against
	// whether every wrong option is accounted for.
	//
	// Depth generalises what used to be an application+hard special case. A slot
	// that was given a second claim requires the question to use it, whatever
	// skill the slot carries — that is the only thing separating a two-claim
	// item from the same claim restated.
	if len(slot.SupportAtomIDs) > 0 && !coversAll(q.SupportingAtomIDs, slot.SupportAtomIDs) {
		res.Reason = fmt.Sprintf("slot %s promises depth %d and requires supporting atoms %v, got %v",
			slot.ID, slot.MinDepth, slot.SupportAtomIDs, q.SupportingAtomIDs)
		return res
	}
	if slot.MinDecoys > 0 && len(q.DecoyValues) < slot.MinDecoys {
		res.Reason = fmt.Sprintf("slot %s requires %d decoy value(s) the solution does not use, got %d",
			slot.ID, slot.MinDecoys, len(q.DecoyValues))
		return res
	}
	if canonicalSkill(slot.Skill) == SkillErrorFinding && strings.TrimSpace(q.FlawedExpression) == "" {
		res.Reason = fmt.Sprintf("error-finding slot %s must state the flawed work its stem shows", slot.ID)
		return res
	}
	if strings.EqualFold(strings.TrimSpace(slot.Discrimination), DiscriminationHigh) {
		if reason := checkHighDiscrimination(q, slot); reason != "" {
			res.Reason = reason
			return res
		}
	}
	if strings.EqualFold(slot.Skill, "application") && (strings.EqualFold(slot.Difficulty, "medium") || strings.EqualFold(slot.Difficulty, "hard")) {
		if strings.TrimSpace(q.ChangedCondition) == "" {
			res.Reason = fmt.Sprintf("%s application slot %s must state the changed condition", slot.Difficulty, slot.ID)
			return res
		}
	}
	// The supporting-atom half of both blocks below is now the generic depth
	// check above; what stays here is what is specific to the skill.
	if strings.EqualFold(slot.Skill, "application") && strings.EqualFold(slot.Difficulty, "hard") {
		if !validReasoningSteps(q.ReasoningSteps) {
			res.Reason = fmt.Sprintf("hard application slot %s needs two distinct reasoning steps", slot.ID)
			return res
		}
	}
	if strings.EqualFold(slot.Skill, "analysis") {
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

// checkHighDiscrimination is the enforceable half of "every option has to look
// possible". It returns the reason the question falls short, or "".
//
// For a numeric item the bar is the strong one: every wrong option must name
// the arithmetic a student would have run to land on it, which the distractor
// path gate has already evaluated and matched to the printed number. Prose
// items have no such handle, so they fall back to one distinct written reason
// per wrong option — weaker, but it is the difference between an option set
// that was thought about and one padded to four.
func checkHighDiscrimination(q Question, slot *CoverageSlot) string {
	idx := q.CorrectIndex()
	if idx < 0 {
		return fmt.Sprintf("slot %s needs one correct choice before its distractors can be judged", slot.ID)
	}
	wrong := len(q.Choices) - 1
	if wrong < 1 {
		return fmt.Sprintf("slot %s has no wrong choices to discriminate against", slot.ID)
	}
	if q.NeedsCalculation() {
		for i, choice := range q.Choices {
			if i == idx {
				continue
			}
			if strings.TrimSpace(choice.DistractorExpression) == "" {
				return fmt.Sprintf("slot %s asks for close distractors: numeric choice %d states no error path that produces it", slot.ID, i+1)
			}
		}
		return ""
	}
	if len(q.DistractorReasons) < wrong {
		return fmt.Sprintf("slot %s asks for close distractors: %d wrong choices but %d distractor_reasons", slot.ID, wrong, len(q.DistractorReasons))
	}
	seen := map[string]bool{}
	for _, reason := range q.DistractorReasons {
		key := strings.ToLower(squeeze(reason))
		if len([]rune(key)) < 8 || seen[key] {
			return fmt.Sprintf("slot %s asks for close distractors: distractor_reasons repeat or are too short to name a mistake", slot.ID)
		}
		seen[key] = true
	}
	return ""
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
