package evidence

import (
	"testing"

	"protoexam/examgen/model"
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
			{ID: "A001", ChunkID: "c1", ConceptIDs: []string{"C001"}, Claim: "a equals v squared over r with v=10 and r=2", Relation: "equation", QuestionForms: []string{"calculation", "application"}},
			{ID: "A002", ChunkID: "c2", ConceptIDs: []string{"C002"}, Claim: "acceleration points inward", Relation: "direction", QuestionForms: []string{"understanding"}},
			{ID: "A003", ChunkID: "c3", ConceptIDs: []string{"C003"}, Claim: "friction supplies the force", Relation: "causal", QuestionForms: []string{"recall", "understanding"}},
			// Two extra atoms so an opportunistic analysis upgrade (which
			// consumes a second atom as support) still leaves enough material
			// for three slots -- this test is about diversity, not analysis.
			{ID: "A004", ChunkID: "c4", ConceptIDs: []string{"C004"}, Claim: "mass resists acceleration", Relation: "observation", QuestionForms: []string{"understanding"}},
			{ID: "A005", ChunkID: "c5", ConceptIDs: []string{"C005"}, Claim: "net force determines motion", Relation: "definition", QuestionForms: []string{"recall"}},
		},
	}
	lesson := Lesson{ID: "L01", ConceptIDs: []string{"C001", "C002", "C003", "C004", "C005"}, ChunkIDs: []string{"c1", "c2", "c3", "c4", "c5"}}
	contract := BuildCoverageContractForRun(lesson, graph, []Chunk{{ID: "c1"}, {ID: "c2"}, {ID: "c3"}, {ID: "c4"}, {ID: "c5"}}, 3, "", false)
	if len(contract.Slots) != 3 {
		t.Fatalf("slots = %#v", contract.Slots)
	}
	calculationFlags := 0
	seenAtoms := map[string]bool{}
	for _, slot := range contract.Slots {
		if seenAtoms[slot.AtomID] {
			t.Fatalf("contract did not diversify: %#v", contract.Slots)
		}
		seenAtoms[slot.AtomID] = true
		if slot.RequiresCalculation {
			calculationFlags++
			if slot.Skill == "calculation" {
				t.Fatalf("calculation leaked into skill dimension: %#v", slot)
			}
		}
	}
	if calculationFlags != 1 {
		t.Fatalf("default contract calculation flags = %d, want one numeric slot: %#v", calculationFlags, contract.Slots)
	}
}

func TestBuildCoverageContractUpgradesToAnalysisAcrossChunks(t *testing.T) {
	graph := &EvidenceGraph{
		Atoms: []EvidenceAtom{
			{ID: "A001", ChunkID: "c1", ConceptIDs: []string{"C001"}, Claim: "friction opposes relative motion", Relation: "causal", QuestionForms: []string{"application", "understanding"}},
			{ID: "A002", ChunkID: "c2", ConceptIDs: []string{"C002"}, Claim: "normal force balances weight on a flat surface", Relation: "condition", QuestionForms: []string{"application", "understanding"}},
		},
	}
	lesson := Lesson{ID: "L01", ChunkIDs: []string{"c1", "c2"}}
	contract := BuildCoverageContractForRun(lesson, graph, []Chunk{{ID: "c1"}, {ID: "c2"}}, 1, "", false)
	if len(contract.Slots) != 1 {
		t.Fatalf("slots = %#v", contract.Slots)
	}
	slot := contract.Slots[0]
	// Difficulty is deliberately left for the writer to report honestly --
	// combining two ideas doesn't by itself make a question hard.
	if slot.Skill != "analysis" {
		t.Fatalf("expected an opportunistic analysis upgrade, got skill=%s: %#v", slot.Skill, slot)
	}
	if slot.Difficulty != "" {
		t.Fatalf("analysis difficulty should be left unpinned in the natural path, got %q: %#v", slot.Difficulty, slot)
	}
	if len(slot.SupportAtomIDs) == 0 {
		t.Fatalf("analysis slot must carry a supporting atom: %#v", slot)
	}
	if len(slot.SourceChunkIDs) < 2 {
		t.Fatalf("analysis slot must span two distinct chunks, got %v", slot.SourceChunkIDs)
	}
}

func TestBuildCoverageContractForRunForcesAnalysisSkill(t *testing.T) {
	graph := &EvidenceGraph{
		Atoms: []EvidenceAtom{
			{ID: "A001", ChunkID: "c1", ConceptIDs: []string{"C001"}, Claim: "friction opposes relative motion", Relation: "causal", QuestionForms: []string{"application", "understanding"}},
			{ID: "A002", ChunkID: "c2", ConceptIDs: []string{"C002"}, Claim: "normal force balances weight on a flat surface", Relation: "condition", QuestionForms: []string{"application", "understanding"}},
			{ID: "A003", ChunkID: "c1", ConceptIDs: []string{"C001"}, Claim: "kinetic friction is weaker than static friction", Relation: "comparison", QuestionForms: []string{"understanding"}},
		},
	}
	lesson := Lesson{ID: "L01", ChunkIDs: []string{"c1", "c2"}}
	contract := BuildCoverageContractForRun(lesson, graph, []Chunk{{ID: "c1"}, {ID: "c2"}}, 2,
		"Set difficulty to hard and skill to analysis.", false)
	if len(contract.Slots) == 0 {
		t.Fatal("expected at least one forced analysis slot")
	}
	for _, slot := range contract.Slots {
		if slot.Skill != "analysis" {
			t.Fatalf("forced analysis directive produced slot with skill=%s: %#v", slot.Skill, slot)
		}
		if len(slot.SourceChunkIDs) < 2 {
			t.Fatalf("forced analysis slot must span two distinct chunks, got %v", slot.SourceChunkIDs)
		}
	}
}

func TestSlotLocalContextKeepsExactAndDropsUnrelatedChunks(t *testing.T) {
	graph := &EvidenceGraph{
		Concepts: []ConceptNode{
			{ID: "C1", ChunkIDs: []string{"c1"}},
			{ID: "C2", ChunkIDs: []string{"c2"}},
			{ID: "C9", ChunkIDs: []string{"c9"}},
		},
		Edges: []ConceptEdge{{From: "C1", To: "C2", Kind: EdgeFollows}},
		Atoms: []EvidenceAtom{{ID: "A1", ChunkID: "c1", ConceptIDs: []string{"C1"}}},
	}
	contract := CoverageContract{Slots: []CoverageSlot{{AtomID: "A1", SourceChunkIDs: []string{"c1"}}}}
	got := SlotLocalContextChunks([]Chunk{{ID: "c9"}, {ID: "c1"}, {ID: "c2"}}, graph, contract)
	if len(got) != 2 || got[0].ID != "c1" || got[1].ID != "c2" {
		t.Fatalf("slot-local context = %#v, want c1,c2", got)
	}
}

func TestPreflightCoverageContractDowngradesUnsupportedDefinitionCalculation(t *testing.T) {
	graph := &EvidenceGraph{Atoms: []EvidenceAtom{{
		ID: "A008", ChunkID: "c1", Relation: "definition", Claim: "The quotient rule is a^(m-n).",
		Quote: "The quotient rule is a^(m-n).", QuestionForms: []string{"application", "understanding"},
	}}}
	contract := CoverageContract{Budget: 1, ContextChunkIDs: []string{"c1"}, Slots: []CoverageSlot{{
		ID: "S01", AtomID: "A008", SourceChunkIDs: []string{"c1"}, Skill: "calculation", EvidenceQuote: "wrong quote",
	}}}
	got := PreflightCoverageContract(contract, graph, []Chunk{{ID: "c1", Text: "The quotient rule is a^(m-n)."}})
	if len(got.Slots) != 1 || got.Slots[0].Skill != "application" {
		t.Fatalf("preflight slot = %#v, want one application slot", got.Slots)
	}
	if got.Slots[0].EvidenceQuote != graph.Atoms[0].Quote || len(got.PreflightChanges) != 2 {
		t.Fatalf("preflight changes = %#v, want quote repair and skill downgrade", got.PreflightChanges)
	}
}

func TestGateSetCoverageAcceptsVolunteeredCalculationBeyondSlotRequirement(t *testing.T) {
	contract := CoverageContract{Slots: []CoverageSlot{{
		ID: "S05", AtomID: "A063", SourceChunkIDs: []string{"c1"}, RequiresCalculation: false,
	}}}
	byChunk := map[string]Chunk{"c1": {ID: "c1"}}
	q := Question{
		CoverageSlotID: "S05", EvidenceAtomID: "A063", EvidenceChunkID: "c1",
		RequiresCalculation: true, Calculation: &model.Calculation{Expression: "2000*30+4000", Expected: 64000},
	}
	res := GateSetCoverage(q, contract, byChunk, map[string]bool{}, map[string]bool{})
	if !res.Pass {
		t.Fatalf("expected a volunteered, verified calculation to pass a non-numeric slot: %#v", res)
	}
}

func TestGateSetCoverageStillRejectsMissingRequiredCalculation(t *testing.T) {
	contract := CoverageContract{Slots: []CoverageSlot{{
		ID: "S05", AtomID: "A063", SourceChunkIDs: []string{"c1"}, RequiresCalculation: true,
	}}}
	byChunk := map[string]Chunk{"c1": {ID: "c1"}}
	q := Question{CoverageSlotID: "S05", EvidenceAtomID: "A063", EvidenceChunkID: "c1", RequiresCalculation: false}
	res := GateSetCoverage(q, contract, byChunk, map[string]bool{}, map[string]bool{})
	if res.Pass {
		t.Fatalf("expected a slot that requires calculation to still reject a question with none: %#v", res)
	}
}

func TestShouldDowngradeCalculationRejectsSymbolicExponentRule(t *testing.T) {
	for _, tc := range []struct {
		name  string
		claim string
	}{
		{"ascii base", "For the quotient rule, b^3 / b^10 = b^-7."},
		{"greek base", "For the quotient rule, θ^3 / θ^10 = θ^-7."},
		{"literal unicode superscript", "For the quotient rule, b³ ÷ b¹⁰ = b⁻⁷."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph := &EvidenceGraph{Atoms: []EvidenceAtom{{
				ID: "A001", ChunkID: "c1", Relation: "equation",
				Claim: tc.claim, Quote: tc.claim,
				QuestionForms: []string{"calculation", "application", "understanding"},
			}}}
			lesson := Lesson{ID: "L01", ConceptIDs: nil, ChunkIDs: []string{"c1"}}
			contract := BuildCoverageContractForRun(lesson, graph, []Chunk{{ID: "c1"}}, 1, "", false)
			if len(contract.Slots) != 1 {
				t.Fatalf("slots = %#v", contract.Slots)
			}
			if contract.Slots[0].RequiresCalculation {
				t.Fatalf("exponent rule with a symbolic (non-numeric) answer should not be assigned a calculation slot: %#v", contract.Slots[0])
			}
		})
	}
}
