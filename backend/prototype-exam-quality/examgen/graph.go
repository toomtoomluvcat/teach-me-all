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

// pedagogyConceptPhrases name teacher-book apparatus rather than subject
// matter: answer keys, assessment rubrics, scoring guides, test banks and
// teaching plans. They are dropped at graph compilation, not only at generation,
// because a concept that survives here costs reduce-step tokens and then offers
// the user a lesson such as "การประเมินผลระบบหายใจ" that can never ship a
// question.
//
// Every phrase below was taken from the 217-concept cached graph of the Thai
// IPST biology teacher book, not invented. Two neighbouring categories are
// deliberately NOT listed: lab activities ("กิจกรรม 15.1 โครงสร้างของหัวใจ...")
// and teacher-knowledge sidebars ("ความรู้เพิ่มเติมสำหรับครู: Osmolarity")
// carry real subject content, and dropping a concept also detaches its chunks
// from every lesson.
// A few entries carry WholeTitle: as a substring they would also swallow real
// content — "เวลาที่ใช้" is a teaching-hours entry on its own but part of a
// legitimate concept in "เวลาที่ใช้ในการย่อยอาหาร".
var pedagogyConceptPhrases = []conceptPhrase{
	{Text: "แนวการวัดและประเมินผล"}, {Text: "การวัดและประเมินผล"}, {Text: "การประเมินผล"},
	{Text: "แบบประเมิน"}, {Text: "เกณฑ์การประเมิน"}, {Text: "การประเมินการนำเสนอ"},
	{Text: "แนวทางการให้คะแนน"},
	{Text: "แบบทดสอบ"}, {Text: "แบบฝึกหัด"}, {Text: "เฉลย"},
	{Text: "แนวการจัดการเรียนรู้"}, {Text: "แนวการจัดกิจกรรม"},
	{Text: "ผลการเรียนรู้"}, {Text: "จุดประสงค์การเรียนรู้"},
	{Text: "การเตรียมตัวล่วงหน้า"}, {Text: "ผังมโนทัศน์"},
	{Text: "เวลาที่ใช้", WholeTitle: true},
	{Text: "ชวนคิด", WholeTitle: true},
	{Text: "การตรวจสอบความเข้าใจ", WholeTitle: true},
	{Text: "answer key"}, {Text: "learning objective"}, {Text: "learning outcome"},
	{Text: "assessment guideline"}, {Text: "assessment criteria"}, {Text: "assessment rubric"},
	{Text: "scoring guide"}, {Text: "scoring rubric"}, {Text: "marking scheme"},
	{Text: "lesson plan"}, {Text: "teaching plan"}, {Text: "teaching guide"}, {Text: "concept map"},
}

type conceptPhrase struct {
	Text string
	// WholeTitle requires the whole label to be this phrase. Without it the
	// phrase matches anywhere in the label.
	WholeTitle bool
}

// IsPedagogyConcept reports whether a pass-1 topic label names teacher-book
// apparatus instead of subject matter. Exported so a cached outline can be
// re-scored without spending a model call.
func IsPedagogyConcept(title string) bool {
	normalised := strings.ToLower(strings.Join(strings.Fields(title), " "))
	if normalised == "" {
		return false
	}
	for _, phrase := range pedagogyConceptPhrases {
		text := strings.ToLower(phrase.Text)
		if phrase.WholeTitle {
			if normalised == text {
				return true
			}
			continue
		}
		if strings.Contains(normalised, text) {
			return true
		}
	}
	return false
}

// CountPedagogyTopics reports how many distinct topic labels BuildEvidenceGraph
// will drop, so a run can say what it removed instead of silently shrinking.
func CountPedagogyTopics(perChunk [][]string) int {
	seen := map[string]bool{}
	for _, topics := range perChunk {
		for _, title := range topics {
			title = strings.TrimSpace(title)
			if title == "" || strings.EqualFold(title, nonContent) {
				continue
			}
			if !IsPedagogyConcept(title) {
				continue
			}
			seen[strings.ToLower(strings.Join(strings.Fields(title), " "))] = true
		}
	}
	return len(seen)
}

// PruneTeacherGuideConcepts applies the same filter to an outline that already
// exists, so a cached pass 1 does not have to be paid for again to benefit from
// a broadened phrase list. Chunks left without a single surviving concept are
// detached from their lesson, and a lesson left without chunks is removed.
//
// It is a no-op on a freshly built outline, because BuildEvidenceGraph already
// dropped those concepts. Running it unconditionally keeps one code path.
func PruneTeacherGuideConcepts(outline *Outline, chunks []Chunk) (droppedConcepts, droppedLessons int) {
	if outline == nil || outline.EvidenceGraph == nil {
		return 0, 0
	}
	graph := outline.EvidenceGraph

	live := map[string]bool{}
	liveChunks := map[string]bool{}
	keptConcepts := graph.Concepts[:0]
	for _, concept := range graph.Concepts {
		if IsPedagogyConcept(concept.Title) {
			droppedConcepts++
			continue
		}
		live[concept.ID] = true
		for _, id := range concept.ChunkIDs {
			liveChunks[id] = true
		}
		keptConcepts = append(keptConcepts, concept)
	}
	if droppedConcepts == 0 {
		return 0, 0
	}
	graph.Concepts = keptConcepts

	keptEdges := graph.Edges[:0]
	for _, edge := range graph.Edges {
		if live[edge.From] && live[edge.To] {
			keptEdges = append(keptEdges, edge)
		}
	}
	graph.Edges = keptEdges

	keptLessons := outline.Lessons[:0]
	for _, lesson := range outline.Lessons {
		conceptIDs := lesson.ConceptIDs[:0]
		for _, id := range lesson.ConceptIDs {
			if live[id] {
				conceptIDs = append(conceptIDs, id)
			}
		}
		lesson.ConceptIDs = conceptIDs

		chunkIDs := lesson.ChunkIDs[:0]
		for _, id := range lesson.ChunkIDs {
			if liveChunks[id] {
				chunkIDs = append(chunkIDs, id)
			}
		}
		lesson.ChunkIDs = chunkIDs

		if len(lesson.ChunkIDs) == 0 {
			droppedLessons++
			continue
		}
		keptLessons = append(keptLessons, lesson)
	}
	outline.Lessons = keptLessons

	for i := range chunks {
		if chunks[i].LessonID != "" && !liveChunks[chunks[i].ID] {
			chunks[i].LessonID = ""
		}
	}
	return droppedConcepts, droppedLessons
}

// BuildEvidenceGraph compiles LLM topic labels into stable concept IDs while
// preserving every source chunk/page that supports each concept. Edges are
// conservative structural facts: concepts co-occur in one chunk or follow one
// another in document order. No semantic relation is invented without source.
//
// Topic labels naming teacher-book apparatus are dropped here. A chunk whose
// only labels were apparatus therefore ends up with no concept, which leaves it
// unassigned to any lesson and out of generation entirely.
func BuildEvidenceGraph(chunks []Chunk, perChunk [][]string) EvidenceGraph {
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
		for _, title := range perChunk[i] {
			title = strings.TrimSpace(title)
			if title == "" || strings.EqualFold(title, nonContent) {
				continue
			}
			if IsPedagogyConcept(title) {
				continue
			}
			key := strings.ToLower(strings.Join(strings.Fields(title), " "))
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
