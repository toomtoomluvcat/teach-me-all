package gates

import "protoexam/examgen/model"

type Chunk = model.Chunk
type Choice = model.Choice
type Calculation = model.Calculation
type Question = model.Question
type Kind = model.Kind
type GateName = model.GateName
type GateResult = model.GateResult
type GateReport = model.GateReport

const (
	KindMCQSingle = model.KindMCQSingle

	GateWellFormed = model.GateWellFormed
	GateSourceRole = model.GateSourceRole
	GateQuote      = model.GateQuote
	GateArithmetic = model.GateArithmetic
	GateUnit       = model.GateUnit
	GateDistinct   = model.GateDistinct
	GateCoverage   = model.GateCoverage
	GateDemand     = model.GateDemand

	GateDecoy          = model.GateDecoy
	GateDistractorPath = model.GateDistractorPath
	GateFlawedWork     = model.GateFlawedWork

	SourceRoleUnknown          = model.SourceRoleUnknown
	SourceRoleCore             = model.SourceRoleCore
	SourceRolePrelearningCheck = model.SourceRolePrelearningCheck
)

type SourceRole = model.SourceRole
type Page = model.Page
type ChunkOptions = model.ChunkOptions
type Arith = model.Arith

func RuneLen(s string) int { return model.RuneLen(s) }

func ChunkPages(pages []Page, opt ChunkOptions) []Chunk { return model.ChunkPages(pages, opt) }

func classifySourceRole(text string) SourceRole { return model.ClassifySourceRole(text) }

func RunDistinctGate(q Question, accepted []Question, vec []float32, acceptedVecs [][]float32) GateResult {
	return gateDistinct(q, accepted, vec, acceptedVecs)
}
