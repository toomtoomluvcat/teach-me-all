package examgen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// UnmarshalJSON accepts both the documented list shape and the compact map
// shape that hosted models sometimes return despite the response schema.
func (v *SourcedVerdict) UnmarshalJSON(data []byte) error {
	var wire struct {
		BestIndex        int             `json:"best_index"`
		AlsoDefensible   []int           `json:"also_defensible"`
		ChoiceVerdicts   json.RawMessage `json:"choice_verdicts"`
		Reason           string          `json:"reason"`
		Dependency       json.RawMessage `json:"dependency"`
		SourceDependency json.RawMessage `json:"source_dependency"`
		DependencyKind   json.RawMessage `json:"dependency_kind"`
		Evidence         json.RawMessage `json:"evidence"`
		Counterfactual   json.RawMessage `json:"counterfactual"`
		DependencyReason json.RawMessage `json:"dependency_reason"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	v.BestIndex = wire.BestIndex
	v.AlsoDefensible = wire.AlsoDefensible
	v.Reason = wire.Reason
	v.SourceDependency = ""
	v.DependencyKind = ""
	v.Evidence = nil
	v.Counterfactual = false
	v.DependencyReason = ""

	dependency := wire.Dependency
	if len(dependency) == 0 || string(dependency) == "null" {
		dependency = wire.SourceDependency
	}
	if source, ok, err := decodeOptionalString(dependency); err != nil {
		var nested struct {
			DependencyKind   json.RawMessage `json:"dependency_kind"`
			Evidence         json.RawMessage `json:"evidence"`
			Counterfactual   json.RawMessage `json:"counterfactual"`
			DependencyReason json.RawMessage `json:"dependency_reason"`
			Reason           json.RawMessage `json:"reason"`
		}
		if nestedErr := json.Unmarshal(wire.SourceDependency, &nested); nestedErr != nil {
			return fmt.Errorf("source_dependency must be a string or object: %w", err)
		}
		kind, _, kindErr := decodeOptionalString(nested.DependencyKind)
		if kindErr != nil {
			return fmt.Errorf("source_dependency.dependency_kind: %w", kindErr)
		}
		if kind == string(SourceDependencySpecific) || kind == string(SourceDependencyGeneric) || kind == string(SourceDependencyUnclear) {
			v.SourceDependency = SourceDependency(kind)
		} else if kind != "" {
			v.DependencyKind = DependencyKind(kind)
		}
		if evidence, evidenceErr := decodeStringList(nested.Evidence); evidenceErr != nil {
			return fmt.Errorf("source_dependency.evidence: %w", evidenceErr)
		} else {
			v.Evidence = evidence
		}
		if counterfactual, counterfactualErr := decodeBoolish(nested.Counterfactual); counterfactualErr != nil {
			return fmt.Errorf("source_dependency.counterfactual: %w", counterfactualErr)
		} else {
			v.Counterfactual = counterfactual
		}
		if reason, _, reasonErr := decodeOptionalString(nested.DependencyReason); reasonErr != nil {
			return fmt.Errorf("source_dependency.dependency_reason: %w", reasonErr)
		} else if reason != "" {
			v.DependencyReason = reason
		} else if reason, _, reasonErr := decodeOptionalString(nested.Reason); reasonErr != nil {
			return fmt.Errorf("source_dependency.reason: %w", reasonErr)
		} else {
			v.DependencyReason = reason
		}
	} else if ok {
		v.SourceDependency = SourceDependency(source)
	}

	if kind, ok, err := decodeOptionalString(wire.DependencyKind); err != nil {
		return fmt.Errorf("dependency_kind: %w", err)
	} else if ok {
		v.DependencyKind = DependencyKind(kind)
	}
	if evidence, err := decodeStringList(wire.Evidence); err != nil {
		return fmt.Errorf("evidence: %w", err)
	} else if evidence != nil {
		v.Evidence = evidence
	}
	if counterfactual, err := decodeBoolish(wire.Counterfactual); err != nil {
		return fmt.Errorf("counterfactual: %w", err)
	} else if len(wire.Counterfactual) > 0 && string(wire.Counterfactual) != "null" {
		v.Counterfactual = counterfactual
	}
	if reason, ok, err := decodeOptionalString(wire.DependencyReason); err != nil {
		return fmt.Errorf("dependency_reason: %w", err)
	} else if ok {
		v.DependencyReason = reason
	}
	v.ChoiceVerdicts = nil
	if len(wire.ChoiceVerdicts) == 0 || string(wire.ChoiceVerdicts) == "null" {
		return nil
	}

	var list []ChoiceVerdict
	if err := json.Unmarshal(wire.ChoiceVerdicts, &list); err == nil {
		v.ChoiceVerdicts = list
		return nil
	}

	var compact map[string]json.RawMessage
	if err := json.Unmarshal(wire.ChoiceVerdicts, &compact); err != nil {
		return fmt.Errorf("choice_verdicts must be a list or index map: %w", err)
	}
	indices := make([]int, 0, len(compact))
	for key := range compact {
		index, err := strconv.Atoi(key)
		if err != nil {
			return fmt.Errorf("choice_verdicts key %q is not an index", key)
		}
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		raw := compact[strconv.Itoa(index)]
		var status ChoiceStatus
		if err := json.Unmarshal(raw, &status); err == nil {
			v.ChoiceVerdicts = append(v.ChoiceVerdicts, ChoiceVerdict{
				Index: index, Status: status,
				Reason: fmt.Sprintf("model classified choice %d as %s", index+1, status),
			})
			continue
		}
		var item ChoiceVerdict
		if err := json.Unmarshal(raw, &item); err != nil {
			return fmt.Errorf("choice_verdicts[%d]: %w", index, err)
		}
		item.Index = index
		if item.Reason == "" {
			item.Reason = fmt.Sprintf("model classified choice %d as %s", index+1, item.Status)
		}
		v.ChoiceVerdicts = append(v.ChoiceVerdicts, item)
	}
	return nil
}

func decodeOptionalString(raw json.RawMessage) (string, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false, err
	}
	return value, true, nil
}

func decodeStringList(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, fmt.Errorf("must be a string or list of strings: %w", err)
	}
	if strings.TrimSpace(one) == "" {
		return nil, nil
	}
	return []string{one}, nil
}

func decodeBoolish(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return false, fmt.Errorf("must be a boolean or string: %w", err)
	}
	trimmed := strings.ToLower(strings.TrimSpace(text))
	switch trimmed {
	case "", "false", "no", "ไม่", "ไม่ใช่":
		return false, nil
	case "true", "yes", "ใช่":
		return true, nil
	default:
		// Some hosted models put the counterfactual explanation in this field
		// instead of a boolean. A non-empty explanation is affirmative evidence
		// that the model attempted the counterfactual check.
		return true, nil
	}
}
