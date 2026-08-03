package examgen

import (
	"encoding/json"
	"testing"
)

func TestTopicUnmarshalToleratesProviderShapes(t *testing.T) {
	cases := []struct {
		name string
		json string
		want Topic
	}{
		{
			name: "classified content",
			json: `{"title":"การย่อยอาหารในปาก","kind":"content"}`,
			want: Topic{Title: "การย่อยอาหารในปาก", Kind: TopicContent},
		},
		{
			name: "classified apparatus",
			json: `{"title":"เฉลยแบบฝึกหัดท้ายบทที่ 13","kind":"APPARATUS"}`,
			want: Topic{Title: "เฉลยแบบฝึกหัดท้ายบทที่ 13", Kind: TopicApparatus},
		},
		// A kind the model invented is not a licence to delete material, so it
		// falls back to content and the question-level gates stay responsible.
		{
			name: "unknown kind falls back to content",
			json: `{"title":"Gas exchange","kind":"pedagogy"}`,
			want: Topic{Title: "Gas exchange", Kind: TopicContent},
		},
		{
			name: "missing kind falls back to content",
			json: `{"title":"Gas exchange"}`,
			want: Topic{Title: "Gas exchange", Kind: TopicContent},
		},
		// The pre-kind wire shape. DeepSeek has already been observed returning
		// an older field layout, so this is a real regression path, not paranoia.
		{
			name: "bare string",
			json: `"Gas exchange"`,
			want: Topic{Title: "Gas exchange", Kind: TopicContent},
		},
		{
			name: "legacy NON_CONTENT sentinel",
			json: `"NON_CONTENT"`,
			want: Topic{Title: "", Kind: TopicNonContent},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got Topic
			if err := json.Unmarshal([]byte(tc.json), &got); err != nil {
				t.Fatalf("Unmarshal(%s) error = %v", tc.json, err)
			}
			if got != tc.want {
				t.Fatalf("Unmarshal(%s) = %#v, want %#v", tc.json, got, tc.want)
			}
		})
	}
}

func TestBuildEvidenceGraphKeepsOnlyContentTopics(t *testing.T) {
	chunks := []Chunk{
		{ID: "p1-c0", Page: 1, Text: "digestion"},
		{ID: "p2-c1", Page: 2, Text: "answers to the chapter exercises"},
		{ID: "p3-c2", Page: 3, Text: "table of contents"},
	}
	perChunk := [][]Topic{
		{
			{Title: "การย่อยอาหารในปาก", Kind: TopicContent},
			{Title: "แนวการวัดและประเมินผล", Kind: TopicApparatus},
		},
		{{Title: "เฉลยแบบฝึกหัดท้ายบทที่ 13", Kind: TopicApparatus}},
		{{Title: "สารบัญ", Kind: TopicNonContent}},
	}

	apparatus, furniture := CountDroppedTopics(perChunk)
	if apparatus != 2 || furniture != 1 {
		t.Fatalf("CountDroppedTopics() = %d apparatus, %d furniture; want 2 and 1", apparatus, furniture)
	}

	graph := BuildEvidenceGraph(chunks, perChunk)
	if len(graph.Concepts) != 1 || graph.Concepts[0].Title != "การย่อยอาหารในปาก" {
		t.Fatalf("concepts = %#v, want only the content topic", graph.Concepts)
	}
	if len(graph.Concepts[0].ChunkIDs) != 1 || graph.Concepts[0].ChunkIDs[0] != "p1-c0" {
		t.Fatalf("provenance = %#v, want the chunk that taught the concept", graph.Concepts[0])
	}
	// A dropped topic must not become an edge endpoint, or the reduce step would
	// see a concept ID the graph no longer contains.
	if len(graph.Edges) != 0 {
		t.Fatalf("edges = %#v, want none", graph.Edges)
	}
}
