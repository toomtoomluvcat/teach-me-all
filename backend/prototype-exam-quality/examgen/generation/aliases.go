package generation

import (
	"protoexam/examgen/evidence"
	"protoexam/examgen/gates"
	"protoexam/examgen/model"
)

type Chunk = model.Chunk
type ChunkTopics = model.ChunkTopics
type Lesson = model.Lesson
type Outline = model.Outline
type Question = model.Question
type Choice = model.Choice
type QualityGrader = model.QualityGrader
type QualityReport = model.QualityReport
type GateReport = model.GateReport
type GateResult = model.GateResult
type GateName = model.GateName
type QualityVerdict = model.QualityVerdict
type Arith = model.Arith
type EvidenceGraph = evidence.EvidenceGraph
type EvidenceAtom = evidence.EvidenceAtom
type CoverageContract = evidence.CoverageContract
type CoverageSlot = evidence.CoverageSlot
type ConceptNode = evidence.ConceptNode
type ConceptEdge = evidence.ConceptEdge
type EdgeKind = evidence.EdgeKind
type Evaluator = gates.Evaluator

const (
	TopicContent    = model.TopicContent
	TopicApparatus  = model.TopicApparatus
	TopicNonContent = model.TopicNonContent
	KindMCQSingle   = model.KindMCQSingle
	GateWellFormed  = model.GateWellFormed
	GateQuote       = model.GateQuote
	GateCoverage    = model.GateCoverage
	EdgeCoOccurs    = evidence.EdgeCoOccurs
	EdgeFollows     = evidence.EdgeFollows
)

func ChunkByID(chunks []Chunk) map[string]Chunk { return model.ChunkByID(chunks) }
func RuneLen(s string) int                      { return model.RuneLen(s) }
func BuildEvidenceGraph(chunks []Chunk, topics []ChunkTopics) EvidenceGraph {
	return evidence.BuildEvidenceGraph(chunks, topics)
}
func KeepEveryChunk(topics []ChunkTopics)                { evidence.KeepEveryChunk(topics) }
func SmoothPassageKinds(topics []ChunkTopics) int        { return evidence.SmoothPassageKinds(topics) }
func CountDroppedTopics(topics []ChunkTopics) (int, int) { return evidence.CountDroppedTopics(topics) }
func checkDropIsPlausible(topics []ChunkTopics) error    { return evidence.CheckDropIsPlausible(topics) }

func LessonContext(lesson Lesson, graph *EvidenceGraph, chunks []Chunk) []Chunk {
	return evidence.LessonContext(lesson, graph, chunks)
}
func RankContextChunks(chunks []Chunk, contract CoverageContract) []Chunk {
	return evidence.RankContextChunks(chunks, contract)
}
func SlotLocalContextChunks(chunks []Chunk, graph *EvidenceGraph, contract CoverageContract) []Chunk {
	return evidence.SlotLocalContextChunks(chunks, graph, contract)
}
func BuildCoverageContractForRun(lesson Lesson, graph *EvidenceGraph, chunks []Chunk, budget int, directive string, forceCalc bool) CoverageContract {
	return evidence.BuildCoverageContractForRun(lesson, graph, chunks, budget, directive, forceCalc)
}
func PreflightCoverageContract(contract CoverageContract, graph *EvidenceGraph, chunks []Chunk) CoverageContract {
	return evidence.PreflightCoverageContract(contract, graph, chunks)
}
func RepairQuestionProvenance(q Question, contract CoverageContract, graph *EvidenceGraph, chunks []Chunk) Question {
	return evidence.RepairQuestionProvenance(q, contract, graph, chunks)
}
func gateSetCoverage(q Question, contract CoverageContract, byChunk map[string]Chunk, usedSlots, usedAtoms map[string]bool) GateResult {
	return evidence.GateSetCoverage(q, contract, byChunk, usedSlots, usedAtoms)
}
func RunCheapGates(q Question, chunk Chunk, ev Evaluator) *GateReport {
	return gates.RunCheapGates(q, chunk, ev)
}
func gateDistinct(q Question, accepted []Question, vec []float32, acceptedVecs [][]float32) GateResult {
	return gates.RunDistinctGate(q, accepted, vec, acceptedVecs)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
