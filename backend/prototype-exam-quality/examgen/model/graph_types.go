package model

// EvidenceAtom is the smallest source-bound claim the compiler can safely
// point back to. It lives with the shared data model so the graph, contracts,
// and JSON verdicts can all use it without importing an orchestration package.
type EvidenceAtom struct {
	ID            string   `json:"id"`
	ChunkID       string   `json:"chunk_id"`
	Page          int      `json:"page"`
	ConceptIDs    []string `json:"concept_ids"`
	Claim         string   `json:"claim"`
	Quote         string   `json:"evidence_quote"`
	Relation      string   `json:"relation"`
	Conditions    []string `json:"conditions"`
	Variables     []string `json:"variables"`
	QuestionForms []string `json:"question_forms"`
}

type EvidenceGraph struct {
	Concepts []ConceptNode  `json:"concepts"`
	Edges    []ConceptEdge  `json:"edges"`
	Atoms    []EvidenceAtom `json:"atoms,omitempty"`
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
