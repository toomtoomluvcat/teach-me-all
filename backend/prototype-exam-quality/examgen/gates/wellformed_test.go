package gates

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

	got := gateWellFormed(q)
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
	if got := gateWellFormed(q); !got.Pass {
		t.Fatalf("imperative math stem was rejected: %#v", got)
	}
}

func TestGateDistinctRejectsSameOperationWithDifferentNumbers(t *testing.T) {
	accepted := []Question{{
		Stem:        "Convert 5 newtons to pounds.",
		Calculation: &Calculation{Expression: "5*0.225", Expected: 1.125},
	}}
	q := Question{
		Stem:        "Convert 2 newtons to pounds.",
		Calculation: &Calculation{Expression: "2*0.225", Expected: 0.45},
	}
	got := gateDistinct(q, accepted, nil, nil)
	if got.Pass {
		t.Fatalf("same N→lb conversion with different numbers passed: %#v", got)
	}
	if !strings.Contains(got.Reason, "same calculation operation") {
		t.Fatalf("reason = %q, want 'same calculation operation'", got.Reason)
	}
}

func TestGateDistinctRejectsSameOperationSharingConstant(t *testing.T) {
	accepted := []Question{{
		Stem:        "What is the weight of a 3.0-kg object on Earth?",
		Calculation: &Calculation{Expression: "3*9.8", Expected: 29.4},
	}}
	q := Question{
		Stem:        "What is the weight of a 5.0-kg object on Earth?",
		Calculation: &Calculation{Expression: "5*9.8", Expected: 49},
	}
	if got := gateDistinct(q, accepted, nil, nil); got.Pass {
		t.Fatalf("same W=mg operation with a shared constant passed: %#v", got)
	}
}

func TestGateDistinctAllowsDifferentOperationsWithSameShape(t *testing.T) {
	accepted := []Question{{
		Stem:        "Convert 5 newtons to pounds.",
		Calculation: &Calculation{Expression: "5*0.225", Expected: 1.125},
	}}
	// Both are N*N but share no constant: N→lb vs W=mg must not collide.
	q := Question{
		Stem:        "What is the weight of a 3.0-kg object on Earth?",
		Calculation: &Calculation{Expression: "3*9.8", Expected: 29.4},
	}
	if got := gateDistinct(q, accepted, nil, nil); !got.Pass {
		t.Fatalf("different operations with the same shape were rejected: %#v", got)
	}
}

func TestGateDistinctAllowsOperandReversal(t *testing.T) {
	accepted := []Question{{
		Stem:        "A 2.0-kg object feels a 19.6 N net force; find its acceleration.",
		Calculation: &Calculation{Expression: "19.6/2", Expected: 9.8},
	}}
	// Same N/N shape, but the operands are swapped: a=F/m is not m=F/a.
	q := Question{
		Stem:        "Given an acceleration of 19.6 m/s² on a 2.0-kg object, what force would produce it?",
		Calculation: &Calculation{Expression: "2/19.6", Expected: 0.102},
	}
	if got := gateDistinct(q, accepted, nil, nil); !got.Pass {
		t.Fatalf("operand reversal was rejected as a duplicate: %#v", got)
	}
}
