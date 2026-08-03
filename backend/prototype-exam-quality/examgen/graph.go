package examgen

import (
	"fmt"
	"strings"
)

type EvidenceGraph struct {
	Concepts []ConceptNode `json:"concepts"`
	Edges    []ConceptEdge `json:"edges"`
}

type ConceptNode struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	ChunkIDs []string `json:"chunk_ids"`
	Pages    []int    `json:"pages"`
}

type EdgeKind string

const (
	EdgeCoOccurs EdgeKind = "co_occurs"
	EdgeFollows  EdgeKind = "follows"
)

type ConceptEdge struct {
	From             string   `json:"from"`
	To               string   `json:"to"`
	Kind             EdgeKind `json:"kind"`
	EvidenceChunkIDs []string `json:"evidence_chunk_ids"`
}

// CountDroppedTopics reports how many distinct topic labels BuildEvidenceGraph
// will refuse, split by why, so a run can say what it removed instead of
// silently shrinking.
func CountDroppedTopics(perChunk [][]Topic) (apparatus, nonContent int) {
	seenApparatus := map[string]bool{}
	seenNonContent := map[string]bool{}
	for _, topics := range perChunk {
		for _, topic := range topics {
			key := conceptKey(topic.Title)
			if key == "" {
				continue
			}
			switch topic.Kind {
			case TopicApparatus:
				seenApparatus[key] = true
			case TopicNonContent:
				seenNonContent[key] = true
			}
		}
	}
	return len(seenApparatus), len(seenNonContent)
}

func conceptKey(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

// BuildEvidenceGraph compiles LLM topic labels into stable concept IDs while
// preserving every source chunk/page that supports each concept. Edges are
// conservative structural facts: concepts co-occur in one chunk or follow one
// another in document order. No semantic relation is invented without source.
//
// Only topics the map step classified as content become concepts. A chunk whose
// labels were all apparatus or page furniture therefore ends up with no concept,
// which leaves it unassigned to any lesson and out of generation entirely.
func BuildEvidenceGraph(chunks []Chunk, perChunk [][]Topic) EvidenceGraph {
	graph := EvidenceGraph{}
	byKey := map[string]int{}
	edgeIndex := map[string]int{}
	previous := ""

	for i, chunk := range chunks {
		if i >= len(perChunk) {
			break
		}
		var chunkConcepts []string
		seenInChunk := map[string]bool{}
		for _, topic := range perChunk[i] {
			if !topic.Teaches() {
				continue
			}
			title := strings.TrimSpace(topic.Title)
			key := conceptKey(title)
			index, exists := byKey[key]
			if !exists {
				index = len(graph.Concepts)
				byKey[key] = index
				graph.Concepts = append(graph.Concepts, ConceptNode{
					ID:    fmt.Sprintf("C%03d", index+1),
					Title: title,
				})
			}
			node := &graph.Concepts[index]
			if !containsString(node.ChunkIDs, chunk.ID) {
				node.ChunkIDs = append(node.ChunkIDs, chunk.ID)
			}
			if !containsInt(node.Pages, chunk.Page) {
				node.Pages = append(node.Pages, chunk.Page)
			}
			if !seenInChunk[node.ID] {
				chunkConcepts = append(chunkConcepts, node.ID)
				seenInChunk[node.ID] = true
			}
		}

		for left := 0; left < len(chunkConcepts); left++ {
			for right := left + 1; right < len(chunkConcepts); right++ {
				addEvidenceEdge(&graph, edgeIndex, chunkConcepts[left], chunkConcepts[right], EdgeCoOccurs, chunk.ID)
			}
		}
		if len(chunkConcepts) > 0 {
			if previous != "" && previous != chunkConcepts[0] {
				addEvidenceEdge(&graph, edgeIndex, previous, chunkConcepts[0], EdgeFollows, chunk.ID)
			}
			previous = chunkConcepts[len(chunkConcepts)-1]
		}
	}
	return graph
}

func addEvidenceEdge(graph *EvidenceGraph, index map[string]int, from, to string, kind EdgeKind, chunkID string) {
	key := string(kind) + "\x00" + from + "\x00" + to
	if i, ok := index[key]; ok {
		if !containsString(graph.Edges[i].EvidenceChunkIDs, chunkID) {
			graph.Edges[i].EvidenceChunkIDs = append(graph.Edges[i].EvidenceChunkIDs, chunkID)
		}
		return
	}
	index[key] = len(graph.Edges)
	graph.Edges = append(graph.Edges, ConceptEdge{
		From:             from,
		To:               to,
		Kind:             kind,
		EvidenceChunkIDs: []string{chunkID},
	})
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
