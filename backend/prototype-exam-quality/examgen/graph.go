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

// maxFlickerRun is how long a stretch of non-content chunks may be and still be
// read as a misclassification rather than a real section.
//
// Measured on the 254-page Thai biology book, where the map step dropped 174 of
// 392 chunks. The run-length histogram splits cleanly: 18 runs of one chunk and
// 7 of two, then real sections of 8, 9, 10, 22 and 34. Every long run is a
// genuine answer key or assessment appendix. 2 recovers 32 chunks without
// touching any of them; 3 starts eating three-chunk answer-key runs, which are
// common at the end of an activity.
const maxFlickerRun = 2

// SmoothPassageKinds rescues short stretches of non-content chunks that sit
// inside content on both sides, and reports how many chunks it recovered.
//
// Teacher-guide apparatus runs in blocks: an answer key covers whole pages, an
// assessment appendix covers a chapter's worth. A single chunk marked apparatus
// between two content chunks is the classifier flickering, not a one-chunk
// answer key — measured cases include the heart on page 112 and blood
// components on page 132.
//
// It only ever rescues. The costs are not symmetric: a rubric that survives is
// caught later by the question-level gates, while a lost passage is lost from
// every lesson silently. Runs at the very start or end of the document are never
// rescued — front matter and the assessment appendix legitimately live there.
func SmoothPassageKinds(perChunk []ChunkTopics) int {
	rescued := 0
	for i := 0; i < len(perChunk); {
		if perChunk[i].Teaches() {
			i++
			continue
		}
		end := i
		for end < len(perChunk) && !perChunk[end].Teaches() {
			end++
		}
		length := end - i
		insideContent := i > 0 && end < len(perChunk)
		if length <= maxFlickerRun && insideContent {
			for j := i; j < end; j++ {
				if len(perChunk[j].Topics) == 0 {
					// Nothing to contribute to the graph, so rescuing it would
					// only inflate the count.
					continue
				}
				perChunk[j].Kind = TopicContent
				rescued++
			}
		}
		i = end
	}
	return rescued
}

// CountDroppedTopics reports how many distinct topic labels BuildEvidenceGraph
// will refuse, split by why, so a run can say what it removed instead of
// silently shrinking.
func CountDroppedTopics(perChunk []ChunkTopics) (apparatus, nonContent int) {
	seenApparatus := map[string]bool{}
	seenNonContent := map[string]bool{}
	for _, labelled := range perChunk {
		if labelled.Teaches() {
			continue
		}
		for _, title := range labelled.Topics {
			key := conceptKey(title)
			if key == "" {
				continue
			}
			if labelled.Kind == TopicApparatus {
				seenApparatus[key] = true
			} else {
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
// Only chunks the map step classified as content contribute concepts. A chunk
// the model called apparatus or page furniture ends up with no concept, which
// leaves it unassigned to any lesson and out of generation entirely.
func BuildEvidenceGraph(chunks []Chunk, perChunk []ChunkTopics) EvidenceGraph {
	graph := EvidenceGraph{}
	byKey := map[string]int{}
	edgeIndex := map[string]int{}
	previous := ""

	for i, chunk := range chunks {
		if i >= len(perChunk) {
			break
		}
		if !perChunk[i].Teaches() {
			continue
		}
		var chunkConcepts []string
		seenInChunk := map[string]bool{}
		for _, title := range perChunk[i].Topics {
			title = strings.TrimSpace(title)
			if title == "" {
				continue
			}
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
