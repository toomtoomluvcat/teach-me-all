package examgen

import (
	"encoding/json"
	"testing"
)

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
