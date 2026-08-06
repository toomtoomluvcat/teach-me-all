package app

import (
	"strings"
	"testing"
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
