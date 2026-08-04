package examgen

import (
	"strings"
	"testing"
)

func TestNormalizeEvidenceGraphAssignsIDsAndDropsUnboundAtoms(t *testing.T) {
	graph := EvidenceGraph{Concepts: []ConceptNode{{ID: "C001", Title: "force"}}}
	chunks := []Chunk{{ID: "c1", Page: 4, Text: "Force equals mass times acceleration."}}
	got, err := NormalizeEvidenceGraph(graph, chunks, []EvidenceAtom{
		{ChunkID: "missing", Claim: "invented", Relation: "fact"},
		{ChunkID: "c1", ConceptIDs: []string{"C001", "C999"}, Claim: "Force equals mass times acceleration.", Quote: "Force equals mass times acceleration.", Relation: "equation", QuestionForms: []string{"calculation"}},
	})
	if err != nil {
		t.Fatalf("NormalizeEvidenceGraph() error = %v", err)
	}
	if len(got.Atoms) != 1 || got.Atoms[0].ID != "A001" || got.Atoms[0].Page != 4 || len(got.Atoms[0].ConceptIDs) != 1 {
		t.Fatalf("normalised atoms = %#v", got.Atoms)
	}
}

func TestLessonContextExpandsOneGraphHopInDocumentOrder(t *testing.T) {
	graph := &EvidenceGraph{
		Concepts: []ConceptNode{
			{ID: "C001", ChunkIDs: []string{"c2"}},
			{ID: "C002", ChunkIDs: []string{"c1"}},
		},
		Edges: []ConceptEdge{{From: "C001", To: "C002", Kind: EdgeFollows}},
	}
	lesson := Lesson{ConceptIDs: []string{"C001"}, ChunkIDs: []string{"c2"}}
	chunks := []Chunk{{ID: "c1", Page: 1, Text: "related"}, {ID: "c2", Page: 2, Text: "lesson"}, {ID: "c3", Page: 3, Text: "unrelated"}}
	got := LessonContext(lesson, graph, chunks)
	if len(got) != 2 || got[0].ID != "c1" || got[1].ID != "c2" {
		t.Fatalf("context = %#v, want c1,c2", got)
	}
}

func TestBuildCoverageContractPrefersDistinctAtomsAndForms(t *testing.T) {
	graph := &EvidenceGraph{
		Atoms: []EvidenceAtom{
			{ID: "A001", ChunkID: "c1", ConceptIDs: []string{"C001"}, Claim: "a equals v squared over r", Relation: "equation", QuestionForms: []string{"calculation", "application"}},
			{ID: "A002", ChunkID: "c2", ConceptIDs: []string{"C002"}, Claim: "acceleration points inward", Relation: "direction", QuestionForms: []string{"understanding"}},
			{ID: "A003", ChunkID: "c3", ConceptIDs: []string{"C003"}, Claim: "friction supplies the force", Relation: "causal", QuestionForms: []string{"recall", "understanding"}},
		},
	}
	lesson := Lesson{ID: "L01", ConceptIDs: []string{"C001", "C002", "C003"}, ChunkIDs: []string{"c1", "c2", "c3"}}
	contract := BuildCoverageContract(lesson, graph, []Chunk{{ID: "c1"}, {ID: "c2"}, {ID: "c3"}}, 3)
	if len(contract.Slots) != 3 {
		t.Fatalf("slots = %#v", contract.Slots)
	}
	seenAtoms := map[string]bool{}
	seenSkills := map[string]bool{}
	for _, slot := range contract.Slots {
		if seenAtoms[slot.AtomID] || seenSkills[slot.Skill] {
			t.Fatalf("contract did not diversify: %#v", contract.Slots)
		}
		seenAtoms[slot.AtomID] = true
		seenSkills[slot.Skill] = true
	}
}

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

func TestSetCandidateScorePrefersAcceptedAndDiverseSets(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{
		{ID: "A001", Relation: "equation"},
		{ID: "A002", Relation: "direction"},
		{ID: "A003", Relation: "causal"},
	}}
	strong := &ExamResult{Passed: []Question{
		{EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "calculation"},
		{EvidenceAtomID: "A002", EvidenceChunkID: "c2", Skill: "understanding"},
	}}
	weak := &ExamResult{Passed: []Question{
		{EvidenceAtomID: "A001", EvidenceChunkID: "c1", Skill: "calculation"},
	}}
	if setCandidateScore(strong, graph) <= setCandidateScore(weak, graph) {
		t.Fatalf("score did not prefer accepted/diverse set: strong=%d weak=%d", setCandidateScore(strong, graph), setCandidateScore(weak, graph))
	}
}
