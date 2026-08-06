package app

import (
	"strings"
	"testing"

	"protoexam/examgen"
)

func TestGenericBenchmarkCasesAreSubjectNeutral(t *testing.T) {
	cases, err := benchmarkCases("all", "eigenvalues", "linear algebra eigenvalues transformations")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 9 || cases[0].LessonContains != "eigenvalues" {
		t.Fatalf("generic benchmark cases = %#v", cases)
	}
	for _, benchmark := range cases {
		lower := strings.ToLower(benchmark.Scope + " " + benchmark.Directive)
		for _, unwanted := range []string{"physics", "newton", "projectile", "force", "mass", "acceleration"} {
			if strings.Contains(lower, unwanted) {
				t.Fatalf("generic benchmark %s contains physics-specific term %q: %s", benchmark.Name, unwanted, lower)
			}
		}
	}
}

func TestCalculationBenchmarkTargetsFlagNotSkill(t *testing.T) {
	cases, err := benchmarkCases("calculation", "eigenvalues", "linear algebra eigenvalues")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || !cases[0].ForceCalc || !cases[0].RequiresCalculation || cases[0].TargetSkill != "" {
		t.Fatalf("calculation benchmark = %#v, want numeric flag without calculation skill", cases)
	}
}

func TestLengthBiasRatioFlagsOnlyDisproportionateKey(t *testing.T) {
	balanced := examgen.Question{Choices: []examgen.Choice{
		{Content: "Because gravity varies slightly over the surface of Earth", IsCorrect: true},
		{Content: "Because the mass of the object changes with location"},
		{Content: "Because air resistance varies with altitude"},
		{Content: "Because the object's volume changes with temperature"},
	}}
	if r := lengthBiasRatio(balanced); r != 0 {
		t.Fatalf("balanced choices flagged as length-biased: ratio=%v", r)
	}

	biased := examgen.Question{Choices: []examgen.Choice{
		{Content: "The oxidation of X removes potential energy from X and the reduced partner gains it, so the net change conserves the total energy of the system", IsCorrect: true},
		{Content: "Molecule X is oxidized in the reaction"},
		{Content: "Molecule X gains an electron during the reaction"},
		{Content: "Molecule X is unchanged by the reaction"},
	}}
	if r := lengthBiasRatio(biased); r <= 1.4 {
		t.Fatalf("disproportionate key not flagged: ratio=%v", r)
	}

	// Short numeric choices are exempt.
	numeric := examgen.Question{Choices: []examgen.Choice{
		{Content: "84 baht", IsCorrect: true},
		{Content: "7 baht"},
		{Content: "120 baht"},
		{Content: "168 baht"},
	}}
	if r := lengthBiasRatio(numeric); r != 0 {
		t.Fatalf("short numeric choices flagged: ratio=%v", r)
	}

	// No key must not panic and must return 0.
	if r := lengthBiasRatio(examgen.Question{Choices: []examgen.Choice{{Content: "a"}}}); r != 0 {
		t.Fatalf("no-key question returned %v", r)
	}
}

func TestMakeBenchmarkCaseResultRecordsLengthBiasAdvisory(t *testing.T) {
	res := &examgen.ExamResult{
		Lesson: examgen.Lesson{Title: "L"},
		Questions: []examgen.Question{
			{
				Choices: []examgen.Choice{
					{Content: "The oxidation of X removes potential energy from X and the reduced partner gains it, so the net change conserves the total energy of the system", IsCorrect: true},
					{Content: "Molecule X is oxidized in the reaction"},
					{Content: "Molecule X gains an electron during the reaction"},
					{Content: "Molecule X is unchanged by the reaction"},
				},
				Report: &examgen.GateReport{Results: []examgen.GateResult{{Gate: examgen.GateWellFormed, Pass: true}}},
			},
		},
	}
	out := makeBenchmarkCaseResult(benchmarkCase{Name: "x"}, res)
	if out.LengthBiasTotal != 1 || out.LengthBiasPassed != 1 || len(out.Questions) != 1 || !out.Questions[0].LengthBias {
		t.Fatalf("length bias not recorded: %#v", out)
	}
	if out.Accepted != 1 {
		t.Fatalf("advisory length bias must not flip Passed; accepted=%d", out.Accepted)
	}
}
