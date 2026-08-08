package app

import (
	"strings"
	"testing"

	"protoexam/examgen"
)

// Every combination the picker offers must resolve to a directive the contract
// builder can read a skill and a difficulty back out of. Without this, a
// renamed benchmark case turns the picker into a set of labels that no longer
// control anything the gates check.
func TestExamDirectiveCoversEveryOfferedCombination(t *testing.T) {
	for _, style := range examStyles() {
		for _, difficulty := range style.Difficulties {
			directive, forceCalc, err := examDirective(style.Skill, difficulty, "Newton's Second Law")
			if err != nil {
				t.Fatalf("%s/%s: %v", style.Skill, difficulty, err)
			}
			if style.Skill == "random" {
				if directive != "" {
					t.Fatalf("random must not carry a directive, got %q", directive)
				}
				continue
			}
			if directive == "" {
				t.Fatalf("%s/%s resolved to an empty directive", style.Skill, difficulty)
			}
			lower := strings.ToLower(directive)
			switch style.Skill {
			case "calculation":
				if !forceCalc {
					t.Fatalf("calculation must force arithmetic")
				}
			case "error-finding":
				if !strings.Contains(lower, "error-finding") {
					t.Fatalf("error-finding directive lost its skill: %q", directive)
				}
			default:
				if !strings.Contains(lower, "skill to "+style.Skill) {
					t.Fatalf("%s/%s directive does not pin the skill: %q", style.Skill, difficulty, directive)
				}
			}
			if difficulty != "" && !strings.Contains(lower, "difficulty to "+difficulty) {
				t.Fatalf("%s/%s directive does not pin the difficulty: %q", style.Skill, difficulty, directive)
			}
		}
	}
}

// A difficulty with no measured directive must be refused rather than mapped
// onto a neighbouring one — an untested instruction is how a picker quietly
// stops matching what was benchmarked.
func TestExamDirectiveRejectsUnmeasuredCombination(t *testing.T) {
	if _, _, err := examDirective("understanding", "hard", "Newton"); err == nil {
		t.Fatal("expected understanding/hard to be refused")
	}
	if _, _, err := examDirective("nonsense", "", "Newton"); err == nil {
		t.Fatal("expected an unknown skill to be refused")
	}
}

func TestLessonToViewSpansItsChunkPages(t *testing.T) {
	chunks := []examgen.Chunk{
		{ID: "c1", Page: 12}, {ID: "c2", Page: 9}, {ID: "c3", Page: 40},
	}
	view := lessonToView(
		examgen.Lesson{ID: "L01", Title: "t", QuestionBudget: 4, ChunkIDs: []string{"c1", "c2"}},
		examgen.ChunkByID(chunks),
	)
	if view.FromPage != 9 || view.ToPage != 12 {
		t.Fatalf("page span = %d-%d, want 9-12", view.FromPage, view.ToPage)
	}
	if view.Chunks != 2 || view.Budget != 4 {
		t.Fatalf("view = %+v", view)
	}
}

// The exam a student sits holds accepted questions only, but the rejected
// drafts must survive into the reviewer panel: they are the evidence that the
// gates did anything at all.
func TestBuildExamViewSplitsAcceptedFromRejected(t *testing.T) {
	pass := &examgen.GateReport{Results: []examgen.GateResult{{Gate: examgen.GateQuote, Pass: true}}}
	fail := &examgen.GateReport{Results: []examgen.GateResult{{Gate: examgen.GateQuote, Pass: false, Reason: "quote not in chunk"}}}

	prep := &preparedDoc{
		Doc:     docRef{Name: "physics.pdf"},
		Outline: &examgen.Outline{CourseTitle: "Physics"},
		Chunks:  []examgen.Chunk{{ID: "c1", Page: 140}},
	}
	res := &examgen.ExamResult{
		Budget: 2,
		Questions: []examgen.Question{
			{Stem: "kept", EvidenceChunkID: "c1", Choices: []examgen.Choice{{Content: "a", IsCorrect: true}, {Content: "b"}}, Report: pass},
			{Stem: "dropped", Report: fail},
		},
	}
	view := buildExamView(prep, examgen.Lesson{ID: "L01", Title: "Newton", ChunkIDs: []string{"c1"}}, res, generateRequest{Count: 2, Skill: "application", Difficulty: "medium"})

	if len(view.Questions) != 1 || view.Questions[0].Stem != "kept" {
		t.Fatalf("accepted = %+v", view.Questions)
	}
	if len(view.Rejected) != 1 || view.Rejected[0].Gates[0].Reason != "quote not in chunk" {
		t.Fatalf("rejected = %+v", view.Rejected)
	}
	if view.Questions[0].Page != 140 {
		t.Fatalf("page = %d, want 140", view.Questions[0].Page)
	}
	if view.Questions[0].Choices[0].Label != "ก" {
		t.Fatalf("label = %q, want ก", view.Questions[0].Choices[0].Label)
	}
}
