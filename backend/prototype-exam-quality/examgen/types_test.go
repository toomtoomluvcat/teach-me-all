package examgen

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
