package examgen

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestChunkTopicsUnmarshalToleratesProviderShapes(t *testing.T) {
	cases := []struct {
		name string
		json string
		want ChunkTopics
	}{
		{
			name: "classified content",
			json: `{"kind":"content","topics":["การย่อยอาหารในปาก"]}`,
			want: ChunkTopics{Kind: TopicContent, Topics: []string{"การย่อยอาหารในปาก"}},
		},
		{
			name: "classified apparatus",
			json: `{"kind":"APPARATUS","topics":["เฉลยแบบฝึกหัดท้ายบทที่ 13"]}`,
			want: ChunkTopics{Kind: TopicApparatus, Topics: []string{"เฉลยแบบฝึกหัดท้ายบทที่ 13"}},
		},
		// A kind the model invented is not a licence to delete a passage, so it
		// falls back to content and the question-level gates stay responsible.
		{
			name: "unknown kind falls back to content",
			json: `{"kind":"pedagogy","topics":["Gas exchange"]}`,
			want: ChunkTopics{Kind: TopicContent, Topics: []string{"Gas exchange"}},
		},
		{
			name: "missing kind falls back to content",
			json: `{"topics":["Gas exchange"]}`,
			want: ChunkTopics{Kind: TopicContent, Topics: []string{"Gas exchange"}},
		},
		// The pre-kind wire shapes. DeepSeek has already been observed returning
		// an older field layout, so these are real regression paths.
		{
			name: "bare topic list",
			json: `["Gas exchange"]`,
			want: ChunkTopics{Kind: TopicContent, Topics: []string{"Gas exchange"}},
		},
		{
			name: "legacy NON_CONTENT sentinel",
			json: `{"topics":["NON_CONTENT"]}`,
			want: ChunkTopics{Kind: TopicNonContent, Topics: []string{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got ChunkTopics
			if err := json.Unmarshal([]byte(tc.json), &got); err != nil {
				t.Fatalf("Unmarshal(%s) error = %v", tc.json, err)
			}
			if got.Kind != tc.want.Kind || !reflect.DeepEqual(got.Topics, tc.want.Topics) {
				t.Fatalf("Unmarshal(%s) = %#v, want %#v", tc.json, got, tc.want)
			}
		})
	}
}

func TestSmoothPassageKindsRescuesOnlyIsolatedChunks(t *testing.T) {
	apparatus := func(title string) ChunkTopics {
		return ChunkTopics{Kind: TopicApparatus, Topics: []string{title}}
	}
	perChunk := []ChunkTopics{
		apparatus("ปกหน้า"), // leading run: front matter, never rescued
		{Kind: TopicContent, Topics: []string{"การย่อยอาหาร"}},
		apparatus("หัวใจ"), // one chunk inside content: flicker
		{Kind: TopicContent, Topics: []string{"หลอดเลือด"}},
		apparatus("เฉลยข้อ 1"), // three-chunk run: a real answer key
		apparatus("เฉลยข้อ 2"),
		apparatus("เฉลยข้อ 3"),
		{Kind: TopicContent, Topics: []string{"ระบบขับถ่าย"}},
		apparatus("เกณฑ์การประเมิน"), // trailing run: assessment appendix
	}

	if rescued := SmoothPassageKinds(perChunk); rescued != 1 {
		t.Fatalf("SmoothPassageKinds() = %d, want 1", rescued)
	}
	if !perChunk[2].Teaches() {
		t.Errorf("the isolated chunk was not rescued: %#v", perChunk[2])
	}
	for _, i := range []int{0, 4, 5, 6, 8} {
		if perChunk[i].Teaches() {
			t.Errorf("chunk %d was rescued; only isolated runs inside content may be", i)
		}
	}
}

func TestSmoothPassageKindsLeavesEmptyChunksAlone(t *testing.T) {
	perChunk := []ChunkTopics{
		{Kind: TopicContent, Topics: []string{"การย่อยอาหาร"}},
		{Kind: TopicNonContent},
		{Kind: TopicContent, Topics: []string{"หลอดเลือด"}},
	}
	if rescued := SmoothPassageKinds(perChunk); rescued != 0 {
		t.Fatalf("SmoothPassageKinds() = %d, want 0: a chunk with no topics adds nothing", rescued)
	}
}

func TestCheckDropIsPlausibleRefusesACollapsedClassification(t *testing.T) {
	chunks := func(dropped, kept int) []ChunkTopics {
		var out []ChunkTopics
		for i := 0; i < dropped; i++ {
			out = append(out, ChunkTopics{Kind: TopicApparatus, Topics: []string{"เฉลย"}})
		}
		for i := 0; i < kept; i++ {
			out = append(out, ChunkTopics{Kind: TopicContent, Topics: []string{"การหายใจ"}})
		}
		return out
	}

	// The measured working run drops a third; half is still allowed so an
	// apparatus-heavy source is not blocked for being what it is.
	if err := checkDropIsPlausible(chunks(2, 4)); err != nil {
		t.Errorf("a third dropped was refused: %v", err)
	}
	if err := checkDropIsPlausible(chunks(3, 3)); err != nil {
		t.Errorf("exactly half dropped was refused: %v", err)
	}
	// The measured broken run dropped 61%.
	err := checkDropIsPlausible(chunks(11, 7))
	if err == nil {
		t.Fatal("61% dropped was accepted; that is the measured signature of a collapsed classifier")
	}
	if !strings.Contains(err.Error(), "--filter-topics=false") {
		t.Errorf("the refusal does not say how to proceed: %v", err)
	}
	if err := checkDropIsPlausible(nil); err != nil {
		t.Errorf("an empty document was refused: %v", err)
	}
}

func TestKeepEveryChunkDisablesFiltering(t *testing.T) {
	perChunk := []ChunkTopics{
		{Kind: TopicApparatus, Topics: []string{"เฉลยแบบฝึกหัด"}},
		{Kind: TopicNonContent, Topics: []string{"สารบัญ"}},
	}
	KeepEveryChunk(perChunk)
	for i, chunk := range perChunk {
		if !chunk.Teaches() {
			t.Errorf("chunk %d still filtered after the human turned filtering off", i)
		}
	}
	if err := checkDropIsPlausible(perChunk); err != nil {
		t.Errorf("guard fired on an unfiltered run: %v", err)
	}
}

func TestBuildEvidenceGraphKeepsOnlyContentChunks(t *testing.T) {
	chunks := []Chunk{
		{ID: "p1-c0", Page: 1, Text: "digestion"},
		{ID: "p2-c1", Page: 2, Text: "answers to the chapter exercises"},
		{ID: "p3-c2", Page: 3, Text: "table of contents"},
	}
	perChunk := []ChunkTopics{
		{Kind: TopicContent, Topics: []string{"การย่อยอาหารในปาก"}},
		{Kind: TopicApparatus, Topics: []string{"เฉลยแบบฝึกหัดท้ายบทที่ 13", "แนวการวัดและประเมินผล"}},
		{Kind: TopicNonContent, Topics: []string{"สารบัญ"}},
	}

	apparatus, furniture := CountDroppedTopics(perChunk)
	if apparatus != 2 || furniture != 1 {
		t.Fatalf("CountDroppedTopics() = %d apparatus, %d furniture; want 2 and 1", apparatus, furniture)
	}

	graph := BuildEvidenceGraph(chunks, perChunk)
	if len(graph.Concepts) != 1 || graph.Concepts[0].Title != "การย่อยอาหารในปาก" {
		t.Fatalf("concepts = %#v, want only the content chunk's topic", graph.Concepts)
	}
	if len(graph.Concepts[0].ChunkIDs) != 1 || graph.Concepts[0].ChunkIDs[0] != "p1-c0" {
		t.Fatalf("provenance = %#v, want the chunk that taught the concept", graph.Concepts[0])
	}
	// A dropped chunk must not become an edge endpoint, or the reduce step would
	// see a concept ID the graph no longer contains.
	if len(graph.Edges) != 0 {
		t.Fatalf("edges = %#v, want none", graph.Edges)
	}
}
