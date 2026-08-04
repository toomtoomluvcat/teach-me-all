package examgen

import (
	"strings"
	"testing"
)

func TestCheckWellFormedRejectsTeacherGuideMetadataQuestion(t *testing.T) {
	q := Question{
		Stem: "ข้อใดเป็นจุดประสงค์การเรียนรู้ของหัวข้อการย่อยอาหารของมนุษย์",
		Choices: []Choice{
			{Content: "อธิบายโครงสร้างของทางเดินอาหาร", IsCorrect: true},
			{Content: "เปรียบเทียบระบบหมุนเวียนเลือด"},
			{Content: "จำแนกชนิดของเนื้อเยื่อพืช"},
			{Content: "คำนวณอัตราการหายใจของเซลล์"},
		},
	}

	got := CheckWellFormed(q)
	if got.Pass {
		t.Fatal("teacher-guide metadata question passed deterministic gate")
	}
	if !strings.Contains(got.Reason, "teacher-guide metadata") {
		t.Fatalf("reason = %q, want teacher-guide metadata", got.Reason)
	}
}

func TestCheckWellFormedAcceptsImperativeMathStem(t *testing.T) {
	q := Question{
		Stem: "Simplify the expression t^23 / t^15 using the quotient rule.",
		Choices: []Choice{
			{Content: "t^8", IsCorrect: true},
			{Content: "t^38"},
			{Content: "t^15"},
			{Content: "t^23"},
		},
	}
	if got := CheckWellFormed(q); !got.Pass {
		t.Fatalf("imperative math stem was rejected: %#v", got)
	}
}
