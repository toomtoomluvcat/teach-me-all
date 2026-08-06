package model

import (
	"encoding/json"
	"testing"
)

func TestSourcedVerdictAcceptsDeepSeekChoiceVerdictMap(t *testing.T) {
	var verdict SourcedVerdict
	err := json.Unmarshal([]byte(`{"choice_verdicts":{"0":"supported","1":"ambiguous","2":"equivalent","3":"unsupported"}}`), &verdict)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(verdict.ChoiceVerdicts) != 4 {
		t.Fatalf("choice verdicts = %#v", verdict.ChoiceVerdicts)
	}
	if verdict.ChoiceVerdicts[2].Index != 2 || verdict.ChoiceVerdicts[2].Status != ChoiceEquivalent || verdict.ChoiceVerdicts[2].Reason == "" {
		t.Fatalf("equivalent verdict = %#v", verdict.ChoiceVerdicts[2])
	}
}

func TestSourcedVerdictPreservesSourceDependencyFields(t *testing.T) {
	var verdict SourcedVerdict
	err := json.Unmarshal([]byte(`{"source_dependency":"specific","dependency_kind":"number","evidence":["2,400 mL"],"counterfactual":true,"dependency_reason":"the value is passage-specific","choice_verdicts":[]}`), &verdict)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if verdict.SourceDependency != SourceDependencySpecific || verdict.DependencyKind != DependencyNumber || !verdict.Counterfactual || len(verdict.Evidence) != 1 || verdict.Evidence[0] != "2,400 mL" {
		t.Fatalf("source dependency = %#v", verdict)
	}
}

func TestSourcedVerdictAcceptsMinimalDependencyString(t *testing.T) {
	var verdict SourcedVerdict
	err := json.Unmarshal([]byte(`{"dependency":"specific","evidence":"2,400 mL"}`), &verdict)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if verdict.SourceDependency != SourceDependencySpecific || len(verdict.Evidence) != 1 || verdict.Evidence[0] != "2,400 mL" {
		t.Fatalf("minimal source dependency = %#v", verdict)
	}
}

func TestSourcedVerdictAcceptsNestedProviderSourceDependencyObject(t *testing.T) {
	var verdict SourcedVerdict
	err := json.Unmarshal([]byte(`{"source_dependency":{"dependency_kind":"specific","evidence":["บีบขวด","สังเกตการเปลี่ยนแปลง"],"counterfactual":"changing this passage fact changes the answer","reason":"the experiment supplies the observation"},"choice_verdicts":[]}`), &verdict)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if verdict.SourceDependency != SourceDependencySpecific || !verdict.Counterfactual || len(verdict.Evidence) != 2 || verdict.DependencyReason == "" {
		t.Fatalf("nested source dependency = %#v", verdict)
	}
}

func TestCalculationUnmarshalAcceptsNumericString(t *testing.T) {
	var got Calculation
	if err := json.Unmarshal([]byte(`{"expression":"2048*1536*48*24*3600","expected":"13063680000000"}`), &got); err != nil {
		t.Fatalf("unmarshal calculation: %v", err)
	}
	if got.Expression != "2048*1536*48*24*3600" || got.Expected != 13063680000000 {
		t.Fatalf("calculation = %#v", got)
	}
}

func TestCalculationUnmarshalRejectsNumberWithUnits(t *testing.T) {
	var got Calculation
	err := json.Unmarshal([]byte(`{"expression":"2+2","expected":"4 bits"}`), &got)
	if err == nil {
		t.Fatal("unmarshal calculation succeeded for a number with units")
	}
}

func TestQuestionUnmarshalNormalizesKeyedDemandLists(t *testing.T) {
	var q Question
	err := json.Unmarshal([]byte(`{"stem":"Which result?","reasoning_steps":{"1":"apply the rule","2":"compare the outcome"},"distractor_reasons":{"A":"wrong assumption","B":"missed condition","C":"wrong operation"}}`), &q)
	if err != nil {
		t.Fatalf("unmarshal keyed demand lists: %v", err)
	}
	if len(q.ReasoningSteps) != 2 || len(q.DistractorReasons) != 3 {
		t.Fatalf("normalized demand lists = %#v", q)
	}
}

func TestQuestionUnmarshalNormalizesArrayOfObjectsDemandLists(t *testing.T) {
	// DeepSeek JSON mode occasionally returns an array of single-key objects
	// where the schema asked for an array of strings. The whole candidate used
	// to be dropped; the list should be normalized and left for the demand gate.
	var q Question
	err := json.Unmarshal([]byte(`{"stem":"Which result?","distractor_reasons":[{"A":"wrong assumption"},{"B":"missed condition"},{"C":"wrong operation"}]}`), &q)
	if err != nil {
		t.Fatalf("unmarshal array-of-objects demand lists: %v", err)
	}
	if len(q.DistractorReasons) != 3 {
		t.Fatalf("normalized array-of-objects list = %#v", q.DistractorReasons)
	}
	want := []string{"wrong assumption", "missed condition", "wrong operation"}
	for i, w := range want {
		if q.DistractorReasons[i] != w {
			t.Fatalf("distractor_reasons[%d] = %q, want %q (list = %#v)", i, q.DistractorReasons[i], w, q.DistractorReasons)
		}
	}
}

func TestQuestionUnmarshalNormalizesArrayOfMultiKeyObjects(t *testing.T) {
	// A keyed label plus an explicit reason key must yield exactly one reason
	// per object, so the demand gate still sees one reason per distractor
	// instead of one per field.
	var q Question
	err := json.Unmarshal([]byte(`{"stem":"Which result?","distractor_reasons":[{"reason":"wrong assumption","choice":"B"},{"reason":"missed condition","choice":"C"}]}`), &q)
	if err != nil {
		t.Fatalf("unmarshal multi-key object list: %v", err)
	}
	if len(q.DistractorReasons) != 2 {
		t.Fatalf("multi-key object list = %#v, want one reason per object", q.DistractorReasons)
	}
	if q.DistractorReasons[0] != "wrong assumption" || q.DistractorReasons[1] != "missed condition" {
		t.Fatalf("multi-key object list = %#v", q.DistractorReasons)
	}
}

func TestQuestionUnmarshalArrayOfObjectsWithoutReasonKeyFallsBack(t *testing.T) {
	// No reason-named key and multiple keys: fall back to all values so no
	// evidence is silently dropped.
	var q Question
	err := json.Unmarshal([]byte(`{"stem":"Which result?","distractor_reasons":[{"note":"wrong assumption","tag":"A"},{"note":"missed condition","tag":"B"}]}`), &q)
	if err != nil {
		t.Fatalf("unmarshal fallback object list: %v", err)
	}
	if len(q.DistractorReasons) != 4 {
		t.Fatalf("fallback object list = %#v, want all values kept", q.DistractorReasons)
	}
}

func TestQuestionCalculationIsOrthogonalToSkill(t *testing.T) {
	var q Question
	if err := json.Unmarshal([]byte(`{"skill":"application","requires_calculation":true}`), &q); err != nil {
		t.Fatalf("unmarshal flagged question: %v", err)
	}
	if q.Skill != "application" || !q.RequiresCalculation || !q.NeedsCalculation() {
		t.Fatalf("flagged question = %#v", q)
	}

	var legacy Question
	if err := json.Unmarshal([]byte(`{"skill":"calculation"}`), &legacy); err != nil {
		t.Fatalf("unmarshal legacy question: %v", err)
	}
	if !legacy.RequiresCalculation || !legacy.NeedsCalculation() {
		t.Fatalf("legacy calculation was not mapped to the flag: %#v", legacy)
	}
}
