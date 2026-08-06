// Package evidence is the source-grounded half of generation: it normalises
// the compiled claim graph, chooses which chunks a lesson may write from, and
// owns the coverage contract that binds every question to one atom.
//
// The files split by responsibility: contract.go builds slots and picks their
// atoms, preflight.go repairs what Go can decide deterministically,
// coverage_gate.go enforces the contract against a written question, and
// graph.go holds graph construction.
package evidence

import (
	"fmt"
	"sort"
	"strings"
)

// CoverageSlot is a deterministic target for one question. It is not a draft:
// it tells the writer which atom, operation, and source provenance to use.
func NormalizeEvidenceGraph(graph EvidenceGraph, chunks []Chunk, atoms []EvidenceAtom) (EvidenceGraph, error) {
	chunkByID := ChunkByID(chunks)
	conceptByID := map[string]bool{}
	for _, concept := range graph.Concepts {
		conceptByID[concept.ID] = true
	}

	clean := make([]EvidenceAtom, 0, len(atoms))
	seen := map[string]bool{}
	for _, atom := range atoms {
		atom.Claim = strings.TrimSpace(atom.Claim)
		atom.Quote = strings.TrimSpace(atom.Quote)
		atom.Relation = strings.TrimSpace(atom.Relation)
		if atom.Claim == "" || atom.Quote == "" || atom.Relation == "" || atom.ChunkID == "" {
			continue
		}
		chunk, ok := chunkByID[atom.ChunkID]
		if !ok {
			continue
		}
		if !strings.Contains(squeeze(chunk.Text), squeeze(atom.Quote)) {
			continue
		}
		atom.Page = chunk.Page
		concepts := atom.ConceptIDs[:0]
		for _, id := range atom.ConceptIDs {
			if conceptByID[id] && !containsString(concepts, id) {
				concepts = append(concepts, id)
			}
		}
		atom.ConceptIDs = concepts
		key := atom.ChunkID + "\x00" + strings.ToLower(atom.Relation) + "\x00" + strings.ToLower(atom.Claim)
		if seen[key] {
			continue
		}
		seen[key] = true
		atom.ID = fmt.Sprintf("A%03d", len(clean)+1)
		clean = append(clean, atom)
	}
	if len(clean) == 0 {
		return graph, fmt.Errorf("evidence compiler returned no source-bound atoms")
	}
	graph.Atoms = clean
	return graph, nil
}

// LessonContext returns the lesson's chunks plus one graph hop of related
// chunks. The result is document ordered and bounded so a large book does not
// turn set generation into a document-sized prompt.
func LessonContext(lesson Lesson, graph *EvidenceGraph, chunks []Chunk) []Chunk {
	if graph == nil {
		return chunksFor(lesson.ChunkIDs, ChunkByID(chunks))
	}

	relevantConcepts := map[string]bool{}
	for _, id := range lesson.ConceptIDs {
		relevantConcepts[id] = true
	}
	for hop := 0; hop < 2; hop++ {
		before := len(relevantConcepts)
		for _, edge := range graph.Edges {
			if relevantConcepts[edge.From] {
				relevantConcepts[edge.To] = true
			}
			if relevantConcepts[edge.To] {
				relevantConcepts[edge.From] = true
			}
		}
		if len(relevantConcepts) == before {
			break
		}
	}

	ids := map[string]bool{}
	for _, id := range lesson.ChunkIDs {
		ids[id] = true
	}
	for _, concept := range graph.Concepts {
		if !relevantConcepts[concept.ID] {
			continue
		}
		for _, id := range concept.ChunkIDs {
			ids[id] = true
		}
	}
	for _, atom := range graph.Atoms {
		for _, conceptID := range atom.ConceptIDs {
			if relevantConcepts[conceptID] {
				ids[atom.ChunkID] = true
				break
			}
		}
	}

	ordered := make([]Chunk, 0, len(ids))
	for _, chunk := range chunks {
		if ids[chunk.ID] {
			ordered = append(ordered, chunk)
		}
	}
	const maxChunks = 24
	const maxRunes = 30_000
	if len(ordered) > maxChunks {
		ordered = ordered[:maxChunks]
	}
	used := 0
	limited := ordered[:0]
	for _, chunk := range ordered {
		if len(limited) > 0 && used+RuneLen(chunk.Text) > maxRunes {
			break
		}
		limited = append(limited, chunk)
		used += RuneLen(chunk.Text)
	}
	return limited
}

// RankContextChunks places the exact chunks selected by the coverage contract
// before incidental graph context. The set still receives the same bounded
// evidence pool, but the rune cap now spends its budget on the claims the
// writer is actually required to answer.
func RankContextChunks(chunks []Chunk, contract CoverageContract) []Chunk {
	if len(chunks) < 2 || len(contract.Slots) == 0 {
		return chunks
	}
	priority := map[string]int{}
	next := 0
	for _, slot := range contract.Slots {
		for _, id := range slot.SourceChunkIDs {
			if _, ok := priority[id]; !ok {
				priority[id] = next
				next++
			}
		}
	}
	ordered := append([]Chunk(nil), chunks...)
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, iok := priority[ordered[i].ID]
		pj, jok := priority[ordered[j].ID]
		if iok != jok {
			return iok
		}
		if iok && pi != pj {
			return pi < pj
		}
		return false
	})
	return ordered
}

// SlotLocalContextChunks narrows a broad lesson context to the exact chunks
// cited by the contract plus a small typed-neighbor fringe. The packet keeps
// the raw chunk for quote verification, while the compiled atoms carry the
// claim vocabulary. Unrelated lesson chunks are not useful generation
// context; they mainly create lost-in-the-middle competition.
func SlotLocalContextChunks(chunks []Chunk, graph *EvidenceGraph, contract CoverageContract) []Chunk {
	if graph == nil || len(contract.Slots) == 0 || len(chunks) < 2 {
		return chunks
	}
	exact := map[string]bool{}
	selectedConcepts := map[string]bool{}
	for _, slot := range contract.Slots {
		for _, chunkID := range slot.SourceChunkIDs {
			exact[chunkID] = true
		}
		for _, atomID := range append([]string{slot.AtomID}, slot.SupportAtomIDs...) {
			for _, atom := range graph.Atoms {
				if atom.ID != atomID {
					continue
				}
				for _, conceptID := range atom.ConceptIDs {
					selectedConcepts[conceptID] = true
				}
			}
		}
	}
	neighborConcepts := map[string]bool{}
	for _, edge := range graph.Edges {
		if selectedConcepts[edge.From] {
			neighborConcepts[edge.To] = true
		}
		if selectedConcepts[edge.To] {
			neighborConcepts[edge.From] = true
		}
	}
	for conceptID := range selectedConcepts {
		neighborConcepts[conceptID] = true
	}
	neighborChunks := map[string]bool{}
	for _, concept := range graph.Concepts {
		if !neighborConcepts[concept.ID] {
			continue
		}
		for _, chunkID := range concept.ChunkIDs {
			neighborChunks[chunkID] = true
		}
	}
	const maxChunks = 10
	const maxRunes = 18_000
	ordered := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if exact[chunk.ID] {
			ordered = append(ordered, chunk)
		}
	}
	for _, chunk := range chunks {
		if exact[chunk.ID] || !neighborChunks[chunk.ID] {
			continue
		}
		if len(ordered) >= maxChunks {
			break
		}
		ordered = append(ordered, chunk)
	}
	used := 0
	limited := ordered[:0]
	for _, chunk := range ordered {
		if len(limited) > 0 && used+RuneLen(chunk.Text) > maxRunes && !exact[chunk.ID] {
			continue
		}
		limited = append(limited, chunk)
		used += RuneLen(chunk.Text)
	}
	return limited
}

// BuildCoverageContractForRun applies explicit benchmark targets before atom
// selection. Selecting the normal mixed contract first and rewriting only the
// slot labels afterwards can leave an application slot pointing at an
// understanding atom, which the coverage gate correctly rejects.
