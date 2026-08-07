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
	contract := slotWithAxes(1, 0, DiscriminationHigh)
	q := axisQuestion()
	if got := runCoverage(q, contract); got.Pass || !strings.Contains(got.Reason, "close distractors") {
		t.Fatalf("close distractors were not required: %#v", got)
	}
	q.DistractorReasons = []string{"confuses the lever with the pulley", "reverses the direction of the force", "uses the load arm as the effort arm"}
	if got := runCoverage(q, contract); !got.Pass {
		t.Fatalf("one distinct reason per wrong choice was rejected: %#v", got)
	}
}

func TestCoverageHighDiscriminationRejectsRepeatedReasons(t *testing.T) {
	q := axisQuestion()
	q.DistractorReasons = []string{"confuses the two devices", "confuses the two devices", "confuses the two devices"}
	if got := runCoverage(q, slotWithAxes(1, 0, DiscriminationHigh)); got.Pass {
		t.Fatal("the same mistake listed three times is one distractor, not three")
	}
}

// On a numeric item the bar is the strong one: every wrong option has to name
// the arithmetic that produces it.
func TestCoverageHighDiscriminationRequiresErrorPathsOnNumericItems(t *testing.T) {
	contract := slotWithAxes(1, 0, DiscriminationHigh)
	contract.Slots[0].RequiresCalculation = true
	q := axisQuestion()
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
