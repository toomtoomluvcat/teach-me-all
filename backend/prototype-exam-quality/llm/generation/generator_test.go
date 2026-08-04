package generation

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"protoexam/examgen"
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

func TestEvidenceBatchesStaySmallAndPreserveChunks(t *testing.T) {
	chunks := make([]examgen.Chunk, 15)
	for i := range chunks {
		chunks[i] = examgen.Chunk{
			ID:   fmt.Sprintf("p%d-c0", i),
			Text: strings.Repeat("e", 1000),
		}
	}

	batches := evidenceBatches(chunks)
	if len(batches) != 4 {
		t.Fatalf("got %d batches, want 4", len(batches))
	}

	seen := 0
	for i, batch := range batches {
		if len(batch) > evidenceBatchMaxChunks {
			t.Errorf("batch %d has %d chunks, want at most %d", i, len(batch), evidenceBatchMaxChunks)
		}
		runes := 0
		for _, chunk := range batch {
			seen++
			runes += examgen.RuneLen(chunk.Text) + examgen.RuneLen(chunk.ID) + 32
		}
		if runes > evidenceBatchMaxRunes {
			t.Errorf("batch %d has %d runes, want at most %d", i, runes, evidenceBatchMaxRunes)
		}
	}
	if seen != len(chunks) {
		t.Fatalf("batches contain %d chunks, want %d", seen, len(chunks))
	}
}
