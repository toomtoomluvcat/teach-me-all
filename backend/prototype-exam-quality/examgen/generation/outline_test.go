package generation

import "testing"

func TestContentChunksKeepsOnlyTeachablePassages(t *testing.T) {
	chunks := []Chunk{{ID: "c1"}, {ID: "c2"}, {ID: "c3"}}
	topics := []ChunkTopics{content("subject"), {Kind: TopicApparatus, Topics: []string{"answer key"}}, {Kind: TopicNonContent}}
	got := contentChunks(chunks, topics)
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("contentChunks() = %#v, want only c1", got)
	}
}
