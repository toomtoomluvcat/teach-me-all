package examgen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// UnmarshalJSON accepts both the documented list shape and the compact map
// shape that hosted models sometimes return despite the response schema.
func (v *SourcedVerdict) UnmarshalJSON(data []byte) error {
	var wire struct {
		BestIndex      int             `json:"best_index"`
		AlsoDefensible []int           `json:"also_defensible"`
		ChoiceVerdicts json.RawMessage `json:"choice_verdicts"`
		Reason         string          `json:"reason"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	v.BestIndex = wire.BestIndex
	v.AlsoDefensible = wire.AlsoDefensible
	v.Reason = wire.Reason
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
