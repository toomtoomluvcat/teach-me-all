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
