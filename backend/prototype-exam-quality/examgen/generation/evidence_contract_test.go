package generation

import (
	"strings"
	"testing"
)

func TestQuestionSetPromptCarriesContractAndCrossContext(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{{ID: "A001", ChunkID: "c1", Relation: "equation", Claim: "a equals v squared over r"}}}
	contract := CoverageContract{Budget: 1, ContextChunkIDs: []string{"c1", "c2"}, Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}, Skill: "calculation", Difficulty: "medium", Operation: "equation", Target: "a equals v squared over r"}}}
	prompt := QuestionSetPrompt(Lesson{Title: "Circular motion"}, graph, []Chunk{{ID: "c1", Page: 1, Text: "first source"}, {ID: "c2", Page: 2, Text: "related source"}}, contract, nil, false)
	for _, want := range []string{"S01", "A001", "c1", "c2", "first source", "related source"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("set prompt omitted %q:\n%s", want, prompt)
		}
	}
	if _, ok := QuestionSetSchema(false)["properties"]; !ok {
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

func TestSetCoverageEnforcesDifficulty(t *testing.T) {
	contract := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}, Skill: "application", Difficulty: "hard", EvidenceQuote: "source claim"}}}
	q := Question{CoverageSlotID: "S01", EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "application", Difficulty: "easy", SourceQuote: "source claim"}
	got := gateSetCoverage(q, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
	if got.Pass || !strings.Contains(got.Reason, "difficulty hard") {
		t.Fatalf("coverage result = %#v, want difficulty rejection", got)
	}
}

func TestCalculationTargetLeavesDifficultyOpenWhenUnspecified(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{{ID: "A001", ChunkID: "c1", Claim: "force equals mass times acceleration", Quote: "force equals mass times acceleration", Relation: "equation", QuestionForms: []string{"calculation"}}}}
	got := BuildCoverageContractForRun(Lesson{ChunkIDs: []string{"c1"}}, graph, []Chunk{{ID: "c1"}}, 1, "Generate calculation questions only. Set skill to calculation.", true)
	if len(got.Slots) != 1 || got.Slots[0].Skill != "calculation" || got.Slots[0].Difficulty != "" {
		t.Fatalf("contract = %#v, want calculation with open difficulty", got)
	}
}

func TestRepairQuestionProvenanceRecoversUniqueQuoteMatch(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{{ID: "A001", ChunkID: "c1", Claim: "force equals mass times acceleration", Quote: "Force equals mass times acceleration.", Relation: "equation", QuestionForms: []string{"calculation"}}}}
	contract := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}, Skill: "calculation", EvidenceQuote: "Force equals mass times acceleration."}}}
	q := Question{Stem: "Calculate force", Skill: "calculation", SourceQuote: "Force equals mass times acceleration."}
	got := RepairQuestionProvenance(q, contract, graph, []Chunk{{ID: "c1", Text: "Force equals mass times acceleration."}})
	if got.CoverageSlotID != "S01" || got.EvidenceAtomID != "A001" || got.EvidenceChunkID != "c1" {
		t.Fatalf("repaired question = %#v", got)
	}
}

func TestRepairQuestionProvenanceLeavesAmbiguousQuoteUnchanged(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{
		{ID: "A001", ChunkID: "c1", Quote: "The force is 10 N.", Relation: "fact", QuestionForms: []string{"calculation"}},
		{ID: "A002", ChunkID: "c1", Quote: "The force is 10 N.", Relation: "example", QuestionForms: []string{"calculation"}},
	}}
	contract := CoverageContract{Slots: []CoverageSlot{{ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"}, Skill: "calculation"}}}
	q := Question{SourceQuote: "The force is 10 N.", Skill: "calculation"}
	got := RepairQuestionProvenance(q, contract, graph, []Chunk{{ID: "c1", Text: "The force is 10 N."}})
	if got.CoverageSlotID != "" || got.EvidenceAtomID != "" || got.EvidenceChunkID != "" {
		t.Fatalf("ambiguous provenance was repaired: %#v", got)
	}
}

func TestSetCandidateScorePrefersAcceptedAndDiverseSets(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{{ID: "A001", Relation: "equation"}, {ID: "A002", Relation: "direction"}, {ID: "A003", Relation: "causal"}}}
	strong := &ExamResult{Passed: []Question{{EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "calculation"}, {EvidenceAtomID: "A002", EvidenceChunkID: "c2", Skill: "understanding"}}}
	weak := &ExamResult{Passed: []Question{{EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "calculation"}}}
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
