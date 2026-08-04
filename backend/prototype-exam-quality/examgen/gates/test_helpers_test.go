package gates

func testQuote() string {
	return "เมื่อหายใจออกปกติจะมีปริมาตรอากาศที่ตกค้างในปอดเป็น 2,400 mL"
}

func passingSourceVerdict() SourcedVerdict {
	return SourcedVerdict{
		BestIndex:        0,
		SourceDependency: SourceDependencySpecific,
		DependencyKind:   DependencyOrder,
		Evidence:         []string{testQuote()},
		Counterfactual:   true,
	}
}
