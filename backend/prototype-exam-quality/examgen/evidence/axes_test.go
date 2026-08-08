package evidence

import (
	"strings"
	"testing"
)

func TestDifficultyFromAxes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		depth int
		decoy int
		disc  string
		want  string
	}{
		{"nothing raised", 1, 0, DiscriminationLow, "easy"},
		{"depth alone", 2, 0, DiscriminationLow, "medium"},
		{"decoys alone", 1, 1, DiscriminationLow, "medium"},
		{"discrimination alone", 1, 0, DiscriminationHigh, "medium"},
		{"two of three", 2, 1, DiscriminationLow, "medium"},
		{"all three", 2, 1, DiscriminationHigh, "hard"},
	} {
		if got := DifficultyFromAxes(tc.depth, tc.decoy, tc.disc); got != tc.want {
			t.Errorf("%s: DifficultyFromAxes(%d,%d,%q) = %q, want %q", tc.name, tc.depth, tc.decoy, tc.disc, got, tc.want)
		}
	}
}

// The invariant that keeps the two directions honest: whatever axes a requested
// difficulty resolves to must derive back to that same difficulty. Without it
// the contract could quietly ask for a configuration that means something else.
func TestAxesForDifficultyRoundTrips(t *testing.T) {
	for _, skill := range []string{"recall", "understanding", "application", "analysis", ""} {
		for _, want := range []string{"easy", "medium", "hard"} {
			depth, decoys, disc := axesForDifficulty(skill, want)
			if got := DifficultyFromAxes(depth, decoys, disc); got != want {
				t.Errorf("skill %q difficulty %q resolved to axes (%d,%d,%q) which derive back to %q",
					skill, want, depth, decoys, disc, got)
			}
		}
	}
}

// A hard retrieval item is the case the single-axis model could not express.
// It must stay one retrieval step and get its difficulty from the other axes.
func TestHardRecallIsNotMadeDeepByStackingSteps(t *testing.T) {
	depth, decoys, disc := axesForDifficulty("recall", "hard")
	if decoys < 1 || !strings.EqualFold(disc, DiscriminationHigh) {
		t.Fatalf("hard recall must raise decoys and discrimination, got (%d,%d,%q)", depth, decoys, disc)
	}
}

func TestAxesForDifficultyUnpinnedReturnsNothing(t *testing.T) {
	depth, decoys, disc := axesForDifficulty("application", "")
	if depth != 0 || decoys != 0 || disc != "" {
		t.Fatalf("an unrequested difficulty must not invent axes, got (%d,%d,%q)", depth, decoys, disc)
	}
}

func TestEvidenceAxesRaiseDiscriminationOnlyWhenSomethingIsConfusable(t *testing.T) {
	primary := EvidenceAtom{ID: "A001", ChunkID: "c1", Relation: "definition", Claim: "a lever amplifies force"}
	alone := []EvidenceAtom{primary}
	if _, _, disc := evidenceAxes(primary, alone, nil); !strings.EqualFold(disc, DiscriminationLow) {
		t.Fatalf("nothing to confuse it with must stay %q, got %q", DiscriminationLow, disc)
	}

	sibling := EvidenceAtom{ID: "A002", ChunkID: "c1", Relation: "definition", Claim: "a pulley changes force direction"}
	if _, _, disc := evidenceAxes(primary, []EvidenceAtom{primary, sibling}, nil); !strings.EqualFold(disc, DiscriminationHigh) {
		t.Fatalf("a same-relation neighbour must raise discrimination, got %q", disc)
	}
}

func TestEvidenceAxesCountClaimsNotSteps(t *testing.T) {
	primary := EvidenceAtom{ID: "A001", ChunkID: "c1", Relation: "causal", Claim: "x"}
	depth, _, _ := evidenceAxes(primary, []EvidenceAtom{primary}, []string{"A002"})
	if depth != 2 {
		t.Fatalf("depth must count the claims the slot commits to, got %d", depth)
	}
}

// slotWithAxes builds a slot whose axes were derived from the material: the
// discrimination tier is a preference, not a requirement. pinnedSlotWithAxes is
// the other case — a run that explicitly asked for this difficulty — and is the
// only one the discrimination bar can reject a question for missing.
func slotWithAxes(depth, decoys int, disc string) CoverageContract {
	return CoverageContract{Slots: []CoverageSlot{{
		ID: "S01", AtomID: "A001", SourceChunkIDs: []string{"c1"},
		Skill: "recall", Operation: "definition", EvidenceQuote: "source claim",
		MinDepth: depth, MinDecoys: decoys, Discrimination: disc,
	}}}
}

func axisQuestion() Question {
	return Question{
		CoverageSlotID: "S01", EvidenceAtomID: "A001", EvidenceChunkID: "c1",
		Skill: "recall", Operation: "definition", SourceQuote: "source claim",
		Choices: []Choice{{Content: "right", IsCorrect: true}, {Content: "wrong a"}, {Content: "wrong b"}, {Content: "wrong c"}},
	}
}

func pinnedSlotWithAxes(depth, decoys int, disc string) CoverageContract {
	contract := slotWithAxes(depth, decoys, disc)
	contract.Slots[0].Difficulty = DifficultyFromAxes(depth, decoys, disc)
	return contract
}

// axisQuestionFor answers a slot that pinned its difficulty, which the coverage
// gate checks separately from the axes.
func axisQuestionFor(contract CoverageContract) Question {
	q := axisQuestion()
	q.Difficulty = contract.Slots[0].Difficulty
	return q
}

// Derived discrimination is advisory. The material happening to hold a
// confusable neighbour is a reason to ask for close distractors, not a reason
// to throw away a question that came back without them — a gate can only
// subtract questions, never improve one.
func TestDerivedDiscriminationIsAdvisory(t *testing.T) {
	contract := slotWithAxes(1, 0, DiscriminationHigh)
	if got := runCoverage(axisQuestion(), contract); !got.Pass {
		t.Fatalf("an unrequested close-distractor preference must not reject: %#v", got)
	}
	pinned := pinnedSlotWithAxes(1, 0, DiscriminationHigh)
	if got := runCoverage(axisQuestionFor(pinned), pinned); got.Pass {
		t.Fatal("a run that explicitly asked for a medium item must still be held to it")
	}
}

func runCoverage(q Question, contract CoverageContract) GateResult {
	return GateSetCoverage(q, contract, map[string]Chunk{"c1": {ID: "c1", Text: "source claim"}}, map[string]bool{}, map[string]bool{})
}

func TestCoverageRejectsMissingDecoys(t *testing.T) {
	got := runCoverage(axisQuestion(), slotWithAxes(1, 1, DiscriminationLow))
	if got.Pass || !strings.Contains(got.Reason, "decoy") {
		t.Fatalf("a slot asking for decoys accepted a question with none: %#v", got)
	}
}

func TestCoverageAcceptsDeclaredDecoys(t *testing.T) {
	q := axisQuestion()
	q.DecoyValues = []string{"0.80"}
	if got := runCoverage(q, slotWithAxes(1, 1, DiscriminationLow)); !got.Pass {
		t.Fatalf("declared decoys were rejected: %#v", got)
	}
}

// Prose items have no expression to point at, so the fallback is one distinct
// written reason per wrong option.
func TestCoverageHighDiscriminationProseFallback(t *testing.T) {
	contract := pinnedSlotWithAxes(1, 0, DiscriminationHigh)
	q := axisQuestionFor(contract)
	if got := runCoverage(q, contract); got.Pass || !strings.Contains(got.Reason, "close distractors") {
		t.Fatalf("close distractors were not required: %#v", got)
	}
	q.DistractorReasons = []string{"confuses the lever with the pulley", "reverses the direction of the force", "uses the load arm as the effort arm"}
	if got := runCoverage(q, contract); !got.Pass {
		t.Fatalf("one distinct reason per wrong choice was rejected: %#v", got)
	}
}

func TestCoverageHighDiscriminationRejectsRepeatedReasons(t *testing.T) {
	contract := pinnedSlotWithAxes(1, 0, DiscriminationHigh)
	q := axisQuestionFor(contract)
	q.DistractorReasons = []string{"confuses the two devices", "confuses the two devices", "confuses the two devices"}
	if got := runCoverage(q, contract); got.Pass {
		t.Fatal("the same mistake listed three times is one distractor, not three")
	}
}

// On a numeric item the bar is the strong one: every wrong option has to name
// the arithmetic that produces it.
func TestCoverageHighDiscriminationRequiresErrorPathsOnNumericItems(t *testing.T) {
	contract := pinnedSlotWithAxes(1, 0, DiscriminationHigh)
	contract.Slots[0].RequiresCalculation = true
	q := axisQuestionFor(contract)
	q.RequiresCalculation = true
	q.Calculation = &Calculation{Expression: "12/5", Expected: 2.4}
	q.DistractorReasons = []string{"a", "b", "c"}
	if got := runCoverage(q, contract); got.Pass || !strings.Contains(got.Reason, "error path") {
		t.Fatalf("numeric distractors without an error path were accepted: %#v", got)
	}
	for i := range q.Choices {
		if !q.Choices[i].IsCorrect {
			q.Choices[i].DistractorExpression = "12*5"
		}
	}
	if got := runCoverage(q, contract); !got.Pass {
		t.Fatalf("declared error paths were rejected: %#v", got)
	}
}

// Depth used to be enforced only for hard application and analysis. A slot that
// was given a second claim now requires it whatever the skill, which is what
// makes a two-claim recall item different from one sentence restated.
func TestCoverageEnforcesDepthForEverySkill(t *testing.T) {
	contract := slotWithAxes(2, 0, DiscriminationLow)
	contract.Slots[0].SupportAtomIDs = []string{"A002"}
	contract.Slots[0].SourceChunkIDs = []string{"c1", "c2"}
	q := axisQuestion()
	if got := runCoverage(q, contract); got.Pass || !strings.Contains(got.Reason, "depth") {
		t.Fatalf("a recall slot promising two claims accepted a one-claim question: %#v", got)
	}
	q.SupportingAtomIDs = []string{"A002"}
	if got := runCoverage(q, contract); !got.Pass {
		t.Fatalf("a two-claim recall question was rejected: %#v", got)
	}
}

// The non-numeric route to a verified distractor. A source with no arithmetic
// in it must still be able to satisfy the discrimination axis, or the axis only
// ever applies to numeric subjects.
func TestCoverageHighDiscriminationAcceptsAtomBackedDistractors(t *testing.T) {
	contract := pinnedSlotWithAxes(1, 0, DiscriminationHigh)
	contract.GraphAtomIDs = []string{"A001", "A002", "A003", "A004"}
	q := axisQuestionFor(contract)
	q.Choices[1].DistractorAtomID = "A002"
	q.Choices[2].DistractorAtomID = "A003"
	q.Choices[3].DistractorAtomID = "A004"
	if got := runCoverage(q, contract); !got.Pass {
		t.Fatalf("atom-backed distractors were rejected: %#v", got)
	}
}

func TestCoverageRejectsInventedDistractorAtom(t *testing.T) {
	contract := slotWithAxes(1, 0, DiscriminationLow)
	contract.GraphAtomIDs = []string{"A001", "A002"}
	q := axisQuestion()
	q.Choices[1].DistractorAtomID = "A999"
	if got := runCoverage(q, contract); got.Pass {
		t.Fatal("a distractor citing a claim that is not in the context must fail")
	}
}

// A wrong option that cites the answer's own claim is the answer written twice.
func TestCoverageRejectsDistractorCitingTheSlotAtom(t *testing.T) {
	contract := slotWithAxes(1, 0, DiscriminationLow)
	contract.GraphAtomIDs = []string{"A001", "A002"}
	q := axisQuestion()
	q.Choices[1].DistractorAtomID = "A001"
	if got := runCoverage(q, contract); got.Pass {
		t.Fatal("a wrong choice must not cite the slot's own atom")
	}
}

func TestCoverageRejectsDistractorAtomOnTheKey(t *testing.T) {
	contract := slotWithAxes(1, 0, DiscriminationLow)
	contract.GraphAtomIDs = []string{"A001", "A002"}
	q := axisQuestion()
	q.Choices[0].DistractorAtomID = "A002"
	if got := runCoverage(q, contract); got.Pass {
		t.Fatal("the key must not be labelled a distractor")
	}
}

// Without a compiled vocabulary there is nothing to check against, and
// rejecting on evidence that was never supplied is not a check.
func TestCoverageSkipsAtomVocabularyCheckWhenContextHasNone(t *testing.T) {
	q := axisQuestion()
	q.Choices[1].DistractorAtomID = "A999"
	if got := runCoverage(q, slotWithAxes(1, 0, DiscriminationLow)); !got.Pass {
		t.Fatalf("no vocabulary means no verdict, got %#v", got)
	}
}

// A self-referential citation is a labelling defect, not a broken question, so
// it is repaired away rather than costing the whole item.
func TestRepairDistractorAtomsStripsCitationsThatCannotBeTrue(t *testing.T) {
	contract := slotWithAxes(1, 0, DiscriminationHigh)
	contract.GraphAtomIDs = []string{"A001", "A002"}
	q := axisQuestion()
	q.Choices[0].DistractorAtomID = "A002" // on the key
	q.Choices[1].DistractorAtomID = "A001" // the slot's own atom
	q.Choices[2].DistractorAtomID = "A404" // not in the context
	q.Choices[3].DistractorAtomID = "A002" // legitimate
	q = RepairDistractorAtoms(q, contract)
	for i, want := range []string{"", "", "", "A002"} {
		if got := q.Choices[i].DistractorAtomID; got != want {
			t.Errorf("choice %d distractor atom = %q, want %q", i+1, got, want)
		}
	}
}

// After repair the question is judged on what is left, not on the label that
// was removed: a slot asking for close distractors falls back to reasons.
func TestRepairedQuestionStillFacesTheDiscriminationBar(t *testing.T) {
	contract := pinnedSlotWithAxes(1, 0, DiscriminationHigh)
	contract.GraphAtomIDs = []string{"A001", "A002"}
	q := axisQuestionFor(contract)
	for i := 1; i < len(q.Choices); i++ {
		q.Choices[i].DistractorAtomID = "A001"
	}
	q = RepairDistractorAtoms(q, contract)
	if got := runCoverage(q, contract); got.Pass {
		t.Fatal("stripping the citations must not also strip the requirement")
	}
	q.DistractorReasons = []string{"confuses the ceiling with the floor", "reverses who benefits", "applies it to the wrong market"}
	if got := runCoverage(q, contract); !got.Pass {
		t.Fatalf("the written-reason fallback was rejected: %#v", got)
	}
}

// A draft that answers a claim no slot asked about still fails, but the failure
// has to name what happened. Reported as an unresolvable quote it looks like
// fabrication, which is a far more alarming defect than the one it has.
func TestCoverageNamesTheClaimWhenNoSlotMatches(t *testing.T) {
	contract := slotWithAxes(1, 0, DiscriminationLow)
	q := axisQuestion()
	q.CoverageSlotID = ""
	q.EvidenceAtomID = "A037"
	got := runCoverage(q, contract)
	if got.Pass {
		t.Fatal("a draft matching no slot must still fail")
	}
	for _, want := range []string{"A037", "no slot asks about", "S01=A001"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("reason %q is missing %q", got.Reason, want)
		}
	}
}

func TestCoverageStillReportsAMalformedSlotID(t *testing.T) {
	q := axisQuestion()
	q.CoverageSlotID = "S99"
	got := runCoverage(q, slotWithAxes(1, 0, DiscriminationLow))
	if got.Pass || !strings.Contains(got.Reason, `unknown coverage slot "S99"`) {
		t.Fatalf("reason = %q", got.Reason)
	}
}
