package generation

import (
	"context"
	"fmt"
	"sync"
)

// BuildOutline runs pass 1: name and classify the topics in every chunk, then
// fold the content ones into lessons, then resolve lesson membership back to
// chunk IDs.
//
// Chunks are returned with LessonID filled in. A chunk whose topics were all
// apparatus or page furniture comes back with an empty LessonID and is never
// used for questions.
func BuildOutline(ctx context.Context, chunks []Chunk, d Deps) (*Outline, []Chunk, error) {
	if d.Gen == nil {
		return nil, nil, fmt.Errorf("no generator configured")
	}

	// map: chunk -> topics, remembering which chunks produced each topic.
	//
	// Every chunk is independent, so this is the easiest place in the pipeline
	// to run concurrently. Results are collected by index and merged in document
	// order afterwards, because lesson ordering depends on it.
	var perChunk []ChunkTopics
	if d.TopicBatcher != nil {
		var err error
		perChunk, err = d.TopicBatcher.BatchTopics(ctx, chunks, d.Log)
		if err != nil {
			return nil, nil, fmt.Errorf("batched topics: %w", err)
		}
		if len(perChunk) != len(chunks) {
			return nil, nil, fmt.Errorf("batched topics returned %d chunk results for %d chunks", len(perChunk), len(chunks))
		}
	} else {
		perChunk = make([]ChunkTopics, len(chunks))
		var (
			wg       sync.WaitGroup
			sem      = make(chan struct{}, d.slots())
			mu       sync.Mutex
			done     int
			firstErr error
		)
		for i, c := range chunks {
			wg.Add(1)
			go func(i int, c Chunk) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				topics, err := d.Gen.Topics(ctx, c)

				mu.Lock()
				defer mu.Unlock()
				done++
				if err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("topics for chunk %s: %w", c.ID, err)
					}
					return
				}
				perChunk[i] = topics
				d.Log.report("outline/map", done, len(chunks), fmt.Sprintf("page %d", c.Page))
			}(i, c)
		}
		wg.Wait()
		if firstErr != nil {
			return nil, nil, firstErr
		}
	}

	if d.KeepAllTopics {
		KeepEveryChunk(perChunk)
		d.Log.report("outline/filter", 0, 0, "apparatus filtering is off; every chunk will be offered as a lesson")
	} else {
		if rescued := SmoothPassageKinds(perChunk); rescued > 0 {
			d.Log.report("outline/smooth", rescued, rescued, "isolated chunks put back: apparatus runs in blocks, single chunks do not")
		}
		if apparatus, furniture := CountDroppedTopics(perChunk); apparatus > 0 || furniture > 0 {
			d.Log.report("outline/filter", apparatus+furniture, apparatus+furniture,
				fmt.Sprintf("%d teacher-guide topics and %d page-furniture topics dropped before reduce", apparatus, furniture))
		}
		if err := checkDropIsPlausible(perChunk); err != nil {
			return nil, nil, err
		}
	}
	graph := BuildEvidenceGraph(chunks, perChunk)
	if len(graph.Concepts) == 0 {
		return nil, nil, fmt.Errorf("pass 1 found no teaching content in %d chunks — check the extracted text before blaming the model", len(chunks))
	}
	{
		d.Log.report("outline/compile", 0, 1, "splitting source into atomic evidence")
		compileChunks := chunks
		if !d.KeepAllTopics {
			compileChunks = contentChunks(chunks, perChunk)
		}
		compiled, err := d.Gen.CompileEvidence(ctx, graph, compileChunks)
		if err != nil {
			return nil, nil, fmt.Errorf("compile evidence graph: %w", err)
		}
		graph = compiled
		d.Log.report("outline/compile", 1, 1, fmt.Sprintf("%d evidence atoms", len(graph.Atoms)))
	}

	// reduce: evidence graph -> lessons.
	d.Log.report("outline/reduce", 0, 1, fmt.Sprintf("%d concepts, %d edges", len(graph.Concepts), len(graph.Edges)))
	outline, membership, err := d.Gen.Outline(ctx, graph)
	if err != nil {
		return nil, nil, fmt.Errorf("outline reduce: %w", err)
	}
	outline.EvidenceGraph = &graph
	d.Log.report("outline/reduce", 1, 1, fmt.Sprintf("%d lessons", len(outline.Lessons)))

	// resolve: lesson -> concept IDs -> source chunks. Stable IDs make this a
	// deterministic graph join; model wording can no longer sever provenance.
	assigned := map[string]string{} // chunk ID -> lesson ID
	conceptByID := make(map[string]ConceptNode, len(graph.Concepts))
	for _, concept := range graph.Concepts {
		conceptByID[concept.ID] = concept
	}
	byID := map[string]*Lesson{}
	for i := range outline.Lessons {
		byID[outline.Lessons[i].ID] = &outline.Lessons[i]
	}
	conceptLesson := map[string]string{}
	for _, m := range membership {
		lesson := byID[m.LessonID]
		if lesson == nil {
			continue
		}
		for _, conceptID := range m.ConceptIDs {
			if _, ok := conceptByID[conceptID]; !ok {
				continue
			}
			if _, taken := conceptLesson[conceptID]; taken {
				continue
			}
			attachConceptToLesson(conceptID, lesson.ID, conceptLesson, byID)
		}
	}
	recoverUnassignedConcepts(graph, conceptLesson, byID)

	for _, concept := range graph.Concepts {
		lesson := byID[conceptLesson[concept.ID]]
		if lesson == nil {
			continue
		}
		for _, id := range concept.ChunkIDs {
			if _, taken := assigned[id]; taken {
				continue
			}
			assigned[id] = lesson.ID
			lesson.ChunkIDs = append(lesson.ChunkIDs, id)
		}
	}

	out := make([]Chunk, len(chunks))
	copy(out, chunks)
	for i := range out {
		out[i].LessonID = assigned[out[i].ID]
	}

	// A lesson with no chunks cannot produce grounded questions; drop it rather
	// than let it sit in the picker as a trap.
	kept := outline.Lessons[:0]
	for _, l := range outline.Lessons {
		if len(l.ChunkIDs) > 0 {
			kept = append(kept, l)
		}
	}
	outline.Lessons = kept
	if len(outline.Lessons) == 0 {
		return nil, nil, fmt.Errorf("every lesson came back with no chunks attached — the reduce step returned no valid concept IDs")
	}

	return outline, out, nil
}

func contentChunks(chunks []Chunk, topics []ChunkTopics) []Chunk {
	if len(chunks) == 0 || len(topics) == 0 {
		return nil
	}
	out := make([]Chunk, 0, len(chunks))
	for i, chunk := range chunks {
		if i < len(topics) && topics[i].Teaches() {
			out = append(out, chunk)
		}
	}
	return out
}

func attachConceptToLesson(conceptID, lessonID string, assigned map[string]string, lessons map[string]*Lesson) {
	lesson := lessons[lessonID]
	if lesson == nil || assigned[conceptID] != "" {
		return
	}
	assigned[conceptID] = lessonID
	lesson.ConceptIDs = append(lesson.ConceptIDs, conceptID)
}

// recoverUnassignedConcepts repairs incomplete LLM reduce output from graph
// evidence. Co-occurrence is strongest, document adjacency is second, and the
// nearest assigned concept in source order is the final deterministic fallback.
func recoverUnassignedConcepts(graph EvidenceGraph, assigned map[string]string, lessons map[string]*Lesson) {
	for _, concept := range graph.Concepts {
		if assigned[concept.ID] != "" {
			continue
		}
		lessonID := neighboringLesson(concept.ID, EdgeCoOccurs, graph.Edges, assigned)
		if lessonID == "" {
			lessonID = neighboringLesson(concept.ID, EdgeFollows, graph.Edges, assigned)
		}
		if lessonID != "" {
			attachConceptToLesson(concept.ID, lessonID, assigned, lessons)
		}
	}
	for i, concept := range graph.Concepts {
		if assigned[concept.ID] != "" {
			continue
		}
		for distance := 1; distance < len(graph.Concepts); distance++ {
			if left := i - distance; left >= 0 && assigned[graph.Concepts[left].ID] != "" {
				attachConceptToLesson(concept.ID, assigned[graph.Concepts[left].ID], assigned, lessons)
				break
			}
			if right := i + distance; right < len(graph.Concepts) && assigned[graph.Concepts[right].ID] != "" {
				attachConceptToLesson(concept.ID, assigned[graph.Concepts[right].ID], assigned, lessons)
				break
			}
		}
	}
}

func neighboringLesson(conceptID string, kind EdgeKind, edges []ConceptEdge, assigned map[string]string) string {
	for _, edge := range edges {
		if edge.Kind != kind {
			continue
		}
		if edge.From == conceptID && assigned[edge.To] != "" {
			return assigned[edge.To]
		}
		if edge.To == conceptID && assigned[edge.From] != "" {
			return assigned[edge.From]
		}
	}
	return ""
}
