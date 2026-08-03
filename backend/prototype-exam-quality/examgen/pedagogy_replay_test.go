package examgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPedagogyFilterOnCachedOutline replays the filter over whatever cached
// full-book outline this machine has, which is the cheapest way to test a
// broadened phrase list: no model call, real pass-1 output.
//
// It asserts the one thing that is dangerous rather than the exact counts —
// dropping a concept also detaches its chunks, so a phrase list that empties a
// lesson full of real concepts has gone too far. A lesson whose only concept was
// apparatus is supposed to disappear; that is the point.
//
// On the Thai IPST biology cache this drops 36 of 217 concepts, frees 78 of 392
// chunks, and empties exactly one lesson: L09 "การประเมินผลระบบหายใจ".
func TestPedagogyFilterOnCachedOutline(t *testing.T) {
	matches, err := filepath.Glob("../.scratch/*/outline-v2.json")
	if err != nil || len(matches) == 0 {
		t.Skip("no cached outline on this machine")
	}

	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var wire struct {
			Outline Outline
			Chunks  []Chunk
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		graph := wire.Outline.EvidenceGraph
		if graph == nil || len(graph.Concepts) == 0 {
			continue
		}

		apparatus := map[string]bool{}
		for _, concept := range graph.Concepts {
			if IsPedagogyConcept(concept.Title) {
				apparatus[concept.ID] = true
				t.Logf("drop %s %s (%d chunks)", concept.ID, concept.Title, len(concept.ChunkIDs))
			}
		}
		t.Logf("%s: concepts %d -> %d", path, len(graph.Concepts), len(graph.Concepts)-len(apparatus))

		for _, lesson := range wire.Outline.Lessons {
			survivors := 0
			for _, id := range lesson.ConceptIDs {
				if !apparatus[id] {
					survivors++
				}
			}
			if survivors > 0 {
				continue
			}
			if len(lesson.ConceptIDs) > 1 {
				t.Errorf("lesson %s %q lost all %d concepts — the phrase list is now eating real material",
					lesson.ID, lesson.Title, len(lesson.ConceptIDs))
			}
			t.Logf("lesson %s %q removed: every concept was apparatus", lesson.ID, lesson.Title)
		}

		// The prune path is what a cached run actually takes, so measure it on
		// the same data instead of trusting that it agrees with the filter.
		before := len(wire.Outline.Lessons)
		concepts, lessons := PruneTeacherGuideConcepts(&wire.Outline, wire.Chunks)
		if concepts != len(apparatus) {
			t.Errorf("PruneTeacherGuideConcepts dropped %d concepts, filter found %d", concepts, len(apparatus))
		}
		if len(wire.Outline.Lessons) != before-lessons {
			t.Errorf("lessons = %d after dropping %d from %d", len(wire.Outline.Lessons), lessons, before)
		}
		for _, lesson := range wire.Outline.Lessons {
			if len(lesson.ChunkIDs) == 0 {
				t.Errorf("lesson %s %q survived with no chunks", lesson.ID, lesson.Title)
			}
		}
		if again, _ := PruneTeacherGuideConcepts(&wire.Outline, wire.Chunks); again != 0 {
			t.Errorf("second prune dropped %d more concepts, want an idempotent pass", again)
		}
		t.Logf("%s: prune dropped %d concepts and %d lessons", path, concepts, lessons)
	}
}
