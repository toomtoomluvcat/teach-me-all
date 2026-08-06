package generation

import (
	"strings"
	"testing"
)

func TestQuestionSetPromptCarriesContractAndCrossContext(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{
		{ID: "A001", ChunkID: "c1", Relation: "equation", Claim: "a equals v squared over r"},
		{ID: "A999", ChunkID: "c2", Relation: "definition", Claim: "unassigned unrelated claim"},
	}}
	contract := CoverageContract{Budget: 1, ContextChunkIDs: []string{"c1", "c2"}, Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}, Skill: "understanding", RequiresCalculation: true, Difficulty: "medium", Operation: "equation", Target: "a equals v squared over r"}}}
	prompt := QuestionSetPrompt(Lesson{Title: "Circular motion"}, graph, []Chunk{{ID: "c1", Page: 1, Text: "first source"}, {ID: "c2", Page: 2, Text: "related source"}}, contract, nil, false)
	for _, want := range []string{"S01", "A001", "c1", "c2", "first source", "related source"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("set prompt omitted %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "unassigned unrelated claim") {
		t.Fatalf("set prompt included an atom outside the assigned slots:\n%s", prompt)
	}
	if _, ok := questionSetSchema(false, nil)["properties"]; !ok {
		t.Fatal("set schema has no properties")
	}
}

func TestQuestionSetPromptCarriesRunDirective(t *testing.T) {
	contract := CoverageContract{GenerationDirective: "Use a genuinely new condition; target application easy."}
	prompt := QuestionSetPrompt(Lesson{Title: "Physics"}, nil, nil, contract, nil, false)
	if !strings.Contains(prompt, "genuinely new condition") {
		t.Fatalf("set prompt omitted run directive: %s", prompt)
	}
}

func TestRankContextChunksPutsContractChunksFirst(t *testing.T) {
	chunks := []Chunk{{ID: "c1"}, {ID: "c2"}, {ID: "c3"}}
	contract := CoverageContract{Slots: []CoverageSlot{{SourceChunkIDs: []string{"c3"}}, {SourceChunkIDs: []string{"c1"}}}}
	got := RankContextChunks(chunks, contract)
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if strings.Join(ids, ",") != "c3,c1,c2" {
		t.Fatalf("ranked context = %v, want [c3 c1 c2]", ids)
	}
}

func TestCoverageDirectiveSelectsAtomsThatSupportTargetSkill(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{
		{ID: "A001", ChunkID: "c1", Claim: "direct fact", Quote: "direct fact", Relation: "definition", QuestionForms: []string{"understanding"}},
		{ID: "A002", ChunkID: "c2", Claim: "change condition", Quote: "change condition", Relation: "condition", QuestionForms: []string{"application"}},
		{ID: "A003", ChunkID: "c3", Claim: "numeric equation", Quote: "numeric equation", Relation: "equation", QuestionForms: []string{"calculation"}},
	}}
	got := BuildCoverageContractForRun(Lesson{ChunkIDs: []string{"c1", "c2", "c3"}}, graph, []Chunk{{ID: "c1"}, {ID: "c2"}, {ID: "c3"}}, 2, "Generate application questions at hard level. Set difficulty to hard and skill to application.", false)
	if len(got.Slots) != 1 || got.Slots[0].AtomID != "A002" || got.Slots[0].Skill != "application" || got.Slots[0].Difficulty != "hard" {
		t.Fatalf("contract = %#v, want only application-capable atom A002 at hard", got)
	}
}

func TestHardApplicationContractAttachesDistinctSupportAtom(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{
		{ID: "A001", ChunkID: "c1", ConceptIDs: []string{"C1"}, Claim: "condition changes outcome", Quote: "condition changes outcome", Relation: "causal", QuestionForms: []string{"application"}},
		{ID: "A002", ChunkID: "c2", ConceptIDs: []string{"C1"}, Claim: "outcome has a constraint", Quote: "outcome has a constraint", Relation: "condition", QuestionForms: []string{"understanding", "application"}},
	}}
	contract := BuildCoverageContractForRun(Lesson{ChunkIDs: []string{"c1", "c2"}}, graph, []Chunk{{ID: "c1"}, {ID: "c2"}}, 1, "Generate application questions at hard level. Set difficulty to hard and skill to application.", false)
	if len(contract.Slots) != 1 || len(contract.Slots[0].SupportAtomIDs) != 1 {
		t.Fatalf("hard contract = %#v, want one primary and one support atom", contract)
	}
	if contract.Slots[0].SupportAtomIDs[0] == contract.Slots[0].AtomID {
		t.Fatalf("hard contract reused primary atom: %#v", contract.Slots[0])
	}
}

func TestCoverageDirectiveInfersApplicationFromMechanismRelation(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{
		{ID: "A001", ChunkID: "c1", Claim: "condition changes the outcome", Quote: "condition changes the outcome", Relation: "causal", QuestionForms: []string{"recall"}},
	}}
	got := BuildCoverageContractForRun(Lesson{ChunkIDs: []string{"c1"}}, graph, []Chunk{{ID: "c1"}}, 1, "Generate application questions at easy level. Set skill to application.", false)
	if len(got.Slots) != 1 || got.Slots[0].AtomID != "A001" || got.Slots[0].Skill != "application" {
		t.Fatalf("contract = %#v, want causal atom inferred as application-capable", got)
	}
}

func TestSetCoverageEnforcesDifficulty(t *testing.T) {
	contract := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}, Skill: "application", Difficulty: "hard", EvidenceQuote: "source claim"}}}
	q := Question{CoverageSlotID: "S01", EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "application", Difficulty: "easy", SourceQuote: "source claim"}
	got := gateSetCoverage(q, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
	if got.Pass || !strings.Contains(got.Reason, "difficulty hard") {
		t.Fatalf("coverage result = %#v, want difficulty rejection", got)
	}
}

func TestSetCoverageEnforcesOperation(t *testing.T) {
	contract := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}, Skill: "understanding", Operation: "sequence", EvidenceQuote: "source claim"}}}
	q := Question{CoverageSlotID: "S01", EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "understanding", SourceQuote: "source claim", Operation: "comparison"}
	got := gateSetCoverage(q, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
	if got.Pass || !strings.Contains(got.Reason, "operation sequence") {
		t.Fatalf("coverage result = %#v, want operation rejection", got)
	}
	q.Operation = "sequence"
	got = gateSetCoverage(q, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
	if !got.Pass {
		t.Fatalf("matching operation was rejected: %#v", got)
	}
}

func TestSetCoverageEnforcesCalculationFlag(t *testing.T) {
	contract := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}, Skill: "application", RequiresCalculation: true, Operation: "equation", EvidenceQuote: "source claim"}}}
	q := Question{CoverageSlotID: "S01", EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "application", Operation: "equation", SourceQuote: "source claim"}
	got := gateSetCoverage(q, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
	if got.Pass || !strings.Contains(got.Reason, "requires_calculation") {
		t.Fatalf("missing calculation flag passed: %#v", got)
	}
	q.RequiresCalculation = true
	got = gateSetCoverage(q, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
	if got.Pass || !strings.Contains(got.Reason, "numeric-required slot") {
		t.Fatalf("flag without calculation payload passed: %#v", got)
	}
}

func TestHardApplicationRequiresDemandContract(t *testing.T) {
	contract := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SupportAtomIDs: []string{"A002"}, SourceChunkIDs: []string{"c1", "c2"}, Skill: "application", Difficulty: "hard", Operation: "causal", EvidenceQuote: "source claim"}}}
	base := Question{CoverageSlotID: "S01", EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "application", Difficulty: "hard", Operation: "causal", SourceQuote: "source claim"}
	got := gateSetCoverage(base, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
	if got.Pass || !strings.Contains(got.Reason, "changed condition") {
		t.Fatalf("hard application without changed condition passed: %#v", got)
	}
	base.ChangedCondition = "the input value changes from the source case"
	base.SupportingAtomIDs = []string{"A002"}
	base.ReasoningSteps = []string{"apply the source condition to the changed value", "compare the resulting outcome with the constraint"}
	got = gateSetCoverage(base, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
	if !got.Pass {
		t.Fatalf("linked hard application was rejected: %#v", got)
	}
}

func TestHardApplicationAllowsExtraSupportingAtomsButNotMissing(t *testing.T) {
	contract := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SupportAtomIDs: []string{"A002"}, SourceChunkIDs: []string{"c1", "c2"}, Skill: "application", Difficulty: "hard", Operation: "causal", EvidenceQuote: "source claim"}}}
	base := Question{CoverageSlotID: "S01", EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "application", Difficulty: "hard", Operation: "causal", SourceQuote: "source claim", ChangedCondition: "the input value changes from the source case", ReasoningSteps: []string{"apply the source condition to the changed value", "compare the resulting outcome with the constraint"}}

	// A draft that volunteers an extra supporting atom is still fully checkable:
	// every required atom is present, so it must not be punished for being more
	// cautious than the slot asked (same principle as the calculation flag fix).
	base.SupportingAtomIDs = []string{"A003", "A002"}
	got := gateSetCoverage(base, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
	if !got.Pass {
		t.Fatalf("hard application with extra supporting atom was rejected: %#v", got)
	}

	// But a draft that omits a required atom hides needed evidence and must fail.
	base.SupportingAtomIDs = []string{"A003"}
	got = gateSetCoverage(base, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
	if got.Pass || !strings.Contains(got.Reason, "requires supporting atoms") {
		t.Fatalf("hard application missing a required atom passed: %#v", got)
	}
}

func TestCalculationFlagLeavesSkillHonestAndDifficultyOpenWhenUnspecified(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{{ID: "A001", ChunkID: "c1", Claim: "force equals mass times acceleration: 10 = 2 * 5", Quote: "force equals mass times acceleration: 10 = 2 * 5", Relation: "equation", QuestionForms: []string{"calculation"}}}}
	got := BuildCoverageContractForRun(Lesson{ChunkIDs: []string{"c1"}}, graph, []Chunk{{ID: "c1"}}, 1, "Generate questions that require arithmetic. Set requires_calculation=true.", true)
	if len(got.Slots) != 1 || got.Slots[0].Skill != "understanding" || !got.Slots[0].RequiresCalculation || got.Slots[0].Difficulty != "" {
		t.Fatalf("contract = %#v, want understanding + calculation flag with open difficulty", got)
	}
}

func TestRepairQuestionProvenanceRecoversUniqueQuoteMatch(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{{ID: "A001", ChunkID: "c1", Claim: "force equals mass times acceleration", Quote: "Force equals mass times acceleration.", Relation: "equation", QuestionForms: []string{"calculation"}}}}
	contract := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}, Skill: "understanding", RequiresCalculation: true, Operation: "equation", EvidenceQuote: "Force equals mass times acceleration."}}}
	q := Question{Stem: "Calculate force", Skill: "understanding", RequiresCalculation: true, SourceQuote: "Force equals mass times acceleration."}
	got := RepairQuestionProvenance(q, contract, graph, []Chunk{{ID: "c1", Text: "Force equals mass times acceleration."}})
	if got.CoverageSlotID != "S01" || got.EvidenceAtomID != "A001" || got.EvidenceChunkID != "c1" || got.Operation != "equation" {
		t.Fatalf("repaired question = %#v", got)
	}
}

func TestRepairQuestionProvenanceLeavesAmbiguousQuoteUnchanged(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{
		{ID: "A001", ChunkID: "c1", Quote: "The force is 10 N.", Relation: "fact", QuestionForms: []string{"calculation"}},
		{ID: "A002", ChunkID: "c1", Quote: "The force is 10 N.", Relation: "example", QuestionForms: []string{"calculation"}},
	}}
	contract := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}, Skill: "understanding", RequiresCalculation: true}}}
	q := Question{SourceQuote: "The force is 10 N.", Skill: "understanding", RequiresCalculation: true}
	got := RepairQuestionProvenance(q, contract, graph, []Chunk{{ID: "c1", Text: "The force is 10 N."}})
	if got.CoverageSlotID != "" || got.EvidenceAtomID != "" || got.EvidenceChunkID != "" {
		t.Fatalf("ambiguous provenance was repaired: %#v", got)
	}
}

func TestSetCandidateScorePrefersAcceptedAndDiverseSets(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{{ID: "A001", Relation: "equation"}, {ID: "A002", Relation: "direction"}, {ID: "A003", Relation: "causal"}}}
	strong := &ExamResult{Passed: []Question{{EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "understanding", RequiresCalculation: true}, {EvidenceAtomID: "A002", EvidenceChunkID: "c2", Skill: "understanding"}}}
	weak := &ExamResult{Passed: []Question{{EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "understanding", RequiresCalculation: true}}}
	if setCandidateScore(strong, graph) <= setCandidateScore(weak, graph) {
		t.Fatalf("score did not prefer accepted/diverse set: strong=%d weak=%d", setCandidateScore(strong, graph), setCandidateScore(weak, graph))
	}
}

func TestSetCandidateScoreUsesQualityOnlyAfterAcceptance(t *testing.T) {
	low := &ExamResult{Passed: []Question{{EvidenceAtomID: "A001", EvidenceChunkID: "c1"}}, Quality: &QualityReport{Verdicts: []QualityVerdict{{QuestionIndex: 0, Score: 4}}, TotalScore: 4, MaxScore: 4}}
	high := &ExamResult{Passed: []Question{{EvidenceAtomID: "A001", EvidenceChunkID: "c1"}, {EvidenceAtomID: "A002", EvidenceChunkID: "c2"}}, Quality: &QualityReport{Verdicts: []QualityVerdict{{QuestionIndex: 0, Score: 0}, {QuestionIndex: 1, Score: 0}}, TotalScore: 0, MaxScore: 8}}
	if setCandidateScore(low, nil) >= setCandidateScore(high, nil) {
		t.Fatalf("quality overrode hard acceptance: low=%d high=%d", setCandidateScore(low, nil), setCandidateScore(high, nil))
	}
	better := &ExamResult{Passed: []Question{{EvidenceAtomID: "A001", EvidenceChunkID: "c1"}}, Quality: &QualityReport{Verdicts: []QualityVerdict{{QuestionIndex: 0, Score: 0}}, TotalScore: 0, MaxScore: 4}}
	if setCandidateScore(low, nil) <= setCandidateScore(better, nil) {
		t.Fatalf("quality did not break equal-acceptance tie: low=%d better=%d", setCandidateScore(low, nil), setCandidateScore(better, nil))
	}
}
