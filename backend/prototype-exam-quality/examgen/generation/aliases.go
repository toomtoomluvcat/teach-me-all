package generation

import (
	"context"

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
type BlindVerdict = model.BlindVerdict
type SourcedVerdict = model.SourcedVerdict
type ChoiceVerdict = model.ChoiceVerdict
type ChoiceStatus = model.ChoiceStatus
type QualityVerdict = model.QualityVerdict
type Arith = model.Arith
type EvidenceGraph = evidence.EvidenceGraph
type EvidenceAtom = evidence.EvidenceAtom
type EvidenceCompiler = evidence.EvidenceCompiler
type CoverageContract = evidence.CoverageContract
type CoverageSlot = evidence.CoverageSlot
type ConceptNode = evidence.ConceptNode
type ConceptEdge = evidence.ConceptEdge
type EdgeKind = evidence.EdgeKind
type Judge = gates.Judge
type Evaluator = gates.Evaluator

const (
	TopicContent              = model.TopicContent
	TopicApparatus            = model.TopicApparatus
	TopicNonContent           = model.TopicNonContent
	KindMCQSingle             = model.KindMCQSingle
	ChoiceSupported           = model.ChoiceSupported
	ChoiceUnsupported         = model.ChoiceUnsupported
	ChoiceEquivalent          = model.ChoiceEquivalent
	ChoiceAmbiguous           = model.ChoiceAmbiguous
	GateWellFormed            = model.GateWellFormed
	GateQuote                 = model.GateQuote
	GateSingleValid           = model.GateSingleValid
	GateSourceSpecific        = model.GateSourceSpecific
	GateBlindAnswer           = model.GateBlindAnswer
	SourceDependencySpecific  = model.SourceDependencySpecific
	SourceDependencyGeneric   = model.SourceDependencyGeneric
	SourceDependencyUnclear   = model.SourceDependencyUnclear
	DependencyNumber          = model.DependencyNumber
	DependencyNamedStructure  = model.DependencyNamedStructure
	DependencyOrder           = model.DependencyOrder
	DependencyCondition       = model.DependencyCondition
	DependencyCauseEffect     = model.DependencyCauseEffect
	DependencyComparison      = model.DependencyComparison
	DependencyLocalDefinition = model.DependencyLocalDefinition
	DependencyNone            = model.DependencyNone
	EdgeCoOccurs              = evidence.EdgeCoOccurs
	EdgeFollows               = evidence.EdgeFollows
)

func ChunkByID(chunks []Chunk) map[string]Chunk { return model.ChunkByID(chunks) }
func RuneLen(s string) int                      { return model.RuneLen(s) }

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

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
func AddJudgeGates(ctx context.Context, rep *GateReport, q Question, chunk Chunk, j Judge) error {
	return gates.AddJudgeGates(ctx, rep, q, chunk, j)
}

func gateDistinct(q Question, accepted []Question, vec []float32, acceptedVecs [][]float32) GateResult {
	return gates.RunDistinctGate(q, accepted, vec, acceptedVecs)
}
