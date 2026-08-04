package evidence

import "protoexam/examgen/model"

type Chunk = model.Chunk
type ChunkTopics = model.ChunkTopics
type Lesson = model.Lesson
type Question = model.Question
type GateResult = model.GateResult
type EvidenceAtom = model.EvidenceAtom
type EvidenceGraph = model.EvidenceGraph
type ConceptNode = model.ConceptNode
type ConceptEdge = model.ConceptEdge
type EdgeKind = model.EdgeKind

const GateCoverage = model.GateCoverage

const (
	TopicContent    = model.TopicContent
	TopicApparatus  = model.TopicApparatus
	TopicNonContent = model.TopicNonContent
)

const (
	EdgeCoOccurs = model.EdgeCoOccurs
	EdgeFollows  = model.EdgeFollows
)

func RuneLen(s string) int { return model.RuneLen(s) }

func ChunkByID(chunks []Chunk) map[string]Chunk { return model.ChunkByID(chunks) }
