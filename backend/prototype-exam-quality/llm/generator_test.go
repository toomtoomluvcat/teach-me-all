package llm

import (
	"encoding/json"
	"testing"
)

func TestStringListishAcceptsArrayStringAndObject(t *testing.T) {
	for _, test := range []struct {
		name string
		json string
		want int
	}{
		{name: "array", json: `["v", "r"]`, want: 2},
		{name: "string", json: `"v, r"`, want: 2},
		{name: "object", json: `{"v":"velocity","r":"radius"}`, want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got stringListish
			if err := json.Unmarshal([]byte(test.json), &got); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if len(got) != test.want {
				t.Fatalf("decoded %v, want %d entries", got, test.want)
			}
		})
	}
}
