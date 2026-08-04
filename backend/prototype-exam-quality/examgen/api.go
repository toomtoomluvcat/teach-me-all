// Package examgen is the stable facade for the prototype's exam-generation
// module. The implementation is grouped below it by responsibility, while
// callers keep the compact API that the prototype started with.
package examgen

import (
	"context"

	"protoexam/examgen/evidence"
	"protoexam/examgen/gates"
	"protoexam/examgen/generation"
	"protoexam/examgen/model"
)

type Page = model.Page
type Chunk = model.Chunk
type ChunkOptions = model.ChunkOptions
type ChunkTopics = model.ChunkTopics
type SourceRole = model.SourceRole
type TopicKind = model.TopicKind
type Lesson = model.Lesson
type Outline = model.Outline
type Kind = model.Kind
type Choice = model.Choice
type Calculation = model.Calculation
type Question = model.Question
type GateName = model.GateName
type GateResult = model.GateResult
type GateReport = model.GateReport
type ChoiceStatus = model.ChoiceStatus
type ChoiceVerdict = model.ChoiceVerdict
type BlindVerdict = model.BlindVerdict
type SourceDependency = model.SourceDependency
type DependencyKind = model.DependencyKind
type SourcedVerdict = model.SourcedVerdict
type QualityGrader = model.QualityGrader
type QualityVerdict = model.QualityVerdict
type QualityReport = model.QualityReport

type EvidenceAtom = model.EvidenceAtom
type EvidenceGraph = model.EvidenceGraph
type ConceptNode = model.ConceptNode
type ConceptEdge = model.ConceptEdge
type EdgeKind = model.EdgeKind
type EvidenceCompiler = evidence.EvidenceCompiler
type CoverageSlot = evidence.CoverageSlot
type CoverageContract = evidence.CoverageContract

type Generator = generation.Generator
type QuestionSetGenerator = generation.QuestionSetGenerator
type RejectedDraft = generation.RejectedDraft
type LessonConcepts = generation.LessonConcepts
type Embedder = generation.Embedder
type TopicBatcher = generation.TopicBatcher
type Progress = generation.Progress
type Deps = generation.Deps
type ExamOptions = generation.ExamOptions
type ExamResult = generation.ExamResult
type QuestionSlot = generation.QuestionSlot
type QuestionPlan = generation.QuestionPlan
type LessonPlanner = generation.LessonPlanner
type Judge = gates.Judge
type Evaluator = gates.Evaluator
type Arith = model.Arith

const (
	SourceRoleUnknown          = model.SourceRoleUnknown
	SourceRoleCore             = model.SourceRoleCore
	SourceRolePrelearningCheck = model.SourceRolePrelearningCheck
	TopicContent               = model.TopicContent
	TopicApparatus             = model.TopicApparatus
	TopicNonContent            = model.TopicNonContent
	KindMCQSingle              = model.KindMCQSingle

	GateWellFormed     = model.GateWellFormed
	GateSourceRole     = model.GateSourceRole
	GateQuote          = model.GateQuote
	GateBlindAnswer    = model.GateBlindAnswer
	GateSourceSpecific = model.GateSourceSpecific
	GateSingleValid    = model.GateSingleValid
	GateArithmetic     = model.GateArithmetic
	GateUnit           = model.GateUnit
	GateDistinct       = model.GateDistinct
	GateCoverage       = model.GateCoverage

	ChoiceSupported   = model.ChoiceSupported
	ChoiceUnsupported = model.ChoiceUnsupported
	ChoiceEquivalent  = model.ChoiceEquivalent
	ChoiceAmbiguous   = model.ChoiceAmbiguous

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

	EdgeCoOccurs = model.EdgeCoOccurs
	EdgeFollows  = model.EdgeFollows
)

func ChunkPages(pages []Page, opt ChunkOptions) []Chunk { return model.ChunkPages(pages, opt) }
func DefaultChunkOptions() ChunkOptions                 { return model.DefaultChunkOptions() }
func NewChunkTopics(kind string, topics []string) ChunkTopics {
	return model.NewChunkTopics(kind, topics)
}
func RuneLen(s string) int                          { return model.RuneLen(s) }
func ChunkByID(chunks []Chunk) map[string]Chunk     { return model.ChunkByID(chunks) }
func ChoiceMentionsNumber(s string, v float64) bool { return model.ChoiceMentionsNumber(s, v) }

func BuildEvidenceGraph(chunks []Chunk, topics []ChunkTopics) EvidenceGraph {
	return evidence.BuildEvidenceGraph(chunks, topics)
}
func NormalizeEvidenceGraph(graph EvidenceGraph, chunks []Chunk, atoms []EvidenceAtom) (EvidenceGraph, error) {
	return evidence.NormalizeEvidenceGraph(graph, chunks, atoms)
}
func LessonContext(lesson Lesson, graph *EvidenceGraph, chunks []Chunk) []Chunk {
	return evidence.LessonContext(lesson, graph, chunks)
}
func BuildCoverageContract(lesson Lesson, graph *EvidenceGraph, chunks []Chunk, budget int) CoverageContract {
	return evidence.BuildCoverageContract(lesson, graph, chunks, budget)
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

func BuildOutline(ctx context.Context, chunks []Chunk, d Deps) (*Outline, []Chunk, error) {
	return generation.BuildOutline(ctx, chunks, d)
}
func GenerateExam(ctx context.Context, outline *Outline, lesson Lesson, chunks []Chunk, d Deps, opt ExamOptions) (*ExamResult, error) {
	return generation.GenerateExam(ctx, outline, lesson, chunks, d, opt)
}
func DefaultExamOptions() ExamOptions { return generation.DefaultExamOptions() }

func TopicSchema() map[string]any              { return generation.TopicSchema() }
func TopicPrompt(c Chunk) string               { return generation.TopicPrompt(c) }
func TopicSystem() string                      { return generation.TopicSystem() }
func TopicBatchSchema() map[string]any         { return generation.TopicBatchSchema() }
func TopicBatchPrompt(chunks []Chunk) string   { return generation.TopicBatchPrompt(chunks) }
func TopicBatchSystem() string                 { return generation.TopicBatchSystem() }
func OutlineSchema() map[string]any            { return generation.OutlineSchema() }
func OutlinePrompt(graph EvidenceGraph) string { return generation.OutlinePrompt(graph) }
func OutlineSystem() string                    { return generation.OutlineSystem() }
func EvidenceCompileSchema() map[string]any    { return generation.EvidenceCompileSchema() }
func EvidenceCompilePrompt(graph EvidenceGraph, chunks []Chunk) string {
	return generation.EvidenceCompilePrompt(graph, chunks)
}
func EvidenceCompileSystem() string      { return generation.EvidenceCompileSystem() }
func QuestionPlanSystem() string         { return generation.QuestionPlanSystem() }
func QuestionPlanSchema() map[string]any { return generation.QuestionPlanSchema() }
func QuestionPlanPrompt(lesson Lesson, graph *EvidenceGraph, chunks []Chunk, budget int) string {
	return generation.QuestionPlanPrompt(lesson, graph, chunks, budget)
}
func QuestionSetSystem() string                       { return generation.QuestionSetSystem() }
func QuestionSetSchema(forceCalc bool) map[string]any { return generation.QuestionSetSchema(forceCalc) }
func QuestionSetSchemaForContract(forceCalc bool, contract CoverageContract) map[string]any {
	return generation.QuestionSetSchemaForContract(forceCalc, contract)
}
func QuestionSetPrompt(lesson Lesson, graph *EvidenceGraph, chunks []Chunk, contract CoverageContract, feedback []RejectedDraft, forceCalc bool) string {
	return generation.QuestionSetPrompt(lesson, graph, chunks, contract, feedback, forceCalc)
}
func QuestionSystem() string                       { return generation.QuestionSystem() }
func QuestionSchema(forceCalc bool) map[string]any { return generation.QuestionSchema(forceCalc) }
func QuestionPrompt(lesson Lesson, graph *EvidenceGraph, c Chunk, feedback []RejectedDraft, want int, forceCalc bool) string {
	return generation.QuestionPrompt(lesson, graph, c, feedback, want, forceCalc)
}
func BlindSystem() string                            { return generation.BlindSystem() }
func BlindSchema(numChoices int) map[string]any      { return generation.BlindSchema(numChoices) }
func BlindPrompt(q Question) string                  { return generation.BlindPrompt(q) }
func SourcedSystem() string                          { return generation.SourcedSystem() }
func SourcedSchema(numChoices ...int) map[string]any { return generation.SourcedSchema(numChoices...) }
func SourcedPrompt(q Question, source string) string { return generation.SourcedPrompt(q, source) }
func QualitySystem() string                          { return generation.QualitySystem() }
func QualitySchema() map[string]any                  { return generation.QualitySchema() }
func QualityPrompt(lesson Lesson, chunks []Chunk, questions []Question) string {
	return generation.QualityPrompt(lesson, chunks, questions)
}

func CheckWellFormed(q Question) GateResult { return gates.CheckWellFormed(q) }
func RunCheapGates(q Question, chunk Chunk, ev Evaluator) *GateReport {
	return gates.RunCheapGates(q, chunk, ev)
}
func RunGates(ctx context.Context, q Question, chunk Chunk, j Judge, ev Evaluator) (*GateReport, error) {
	return gates.RunGates(ctx, q, chunk, j, ev)
}
