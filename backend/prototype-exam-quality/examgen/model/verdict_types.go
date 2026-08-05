package model

type BlindVerdict struct {
	Interpretable bool   `json:"interpretable"`
	Reason        string `json:"reason"`
}

type SourceDependency string

const (
	SourceDependencySpecific SourceDependency = "specific"
	SourceDependencyGeneric  SourceDependency = "generic"
	SourceDependencyUnclear  SourceDependency = "unclear"
)

type DependencyKind string

const (
	DependencyNumber          DependencyKind = "number"
	DependencyNamedStructure  DependencyKind = "named_structure"
	DependencyOrder           DependencyKind = "order"
	DependencyCondition       DependencyKind = "condition"
	DependencyCauseEffect     DependencyKind = "cause_effect"
	DependencyComparison      DependencyKind = "comparison"
	DependencyLocalDefinition DependencyKind = "local_definition"
	DependencyNone            DependencyKind = "none"
)

type SourcedVerdict struct {
	BestIndex        int              `json:"best_index"`
	AlsoDefensible   []int            `json:"also_defensible"`
	ChoiceVerdicts   []ChoiceVerdict  `json:"choice_verdicts"`
	Reason           string           `json:"reason"`
	SourceDependency SourceDependency `json:"dependency"`
	DependencyKind   DependencyKind   `json:"dependency_kind"`
	Evidence         []string         `json:"evidence"`
	Counterfactual   bool             `json:"counterfactual"`
	DependencyReason string           `json:"dependency_reason"`
}

type ChoiceStatus string

const (
	ChoiceSupported   ChoiceStatus = "supported"
	ChoiceUnsupported ChoiceStatus = "unsupported"
	ChoiceEquivalent  ChoiceStatus = "equivalent"
	ChoiceAmbiguous   ChoiceStatus = "ambiguous"
)

type ChoiceVerdict struct {
	Index  int          `json:"index"`
	Status ChoiceStatus `json:"status"`
	Reason string       `json:"reason"`
}
