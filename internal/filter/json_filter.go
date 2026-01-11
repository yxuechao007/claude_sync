package filter

import (
	"encoding/json"
	"fmt"

	"github.com/user/claude-sync/internal/config"
)

// FilterJSON filters a JSON object based on the filter configuration
// If filter is nil, returns the original JSON unchanged
func FilterJSON(data []byte, filter *config.FilterConfig) ([]byte, error) {
	if filter == nil {
		return data, nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	filtered := filterObject(obj, filter)

	result, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal filtered JSON: %w", err)
	}

	return result, nil
}

// filterObject filters a map based on include/exclude rules
func filterObject(obj map[string]interface{}, filter *config.FilterConfig) map[string]interface{} {
	result := make(map[string]interface{})

	// If include_fields is specified, only include those fields
	if len(filter.IncludeFields) > 0 {
		includeSet := make(map[string]bool)
		for _, field := range filter.IncludeFields {
			includeSet[field] = true
		}

		for key, value := range obj {
			if includeSet[key] {
				result[key] = value
			}
		}
		return result
	}

	// If exclude_fields is specified, include everything except those fields
	if len(filter.ExcludeFields) > 0 {
		excludeSet := make(map[string]bool)
		for _, field := range filter.ExcludeFields {
			excludeSet[field] = true
		}

		for key, value := range obj {
			if !excludeSet[key] {
				result[key] = value
			}
		}
		return result
	}

	// No filter rules, return original
	return obj
}

// MergeJSON merges filtered JSON back into the original file
// This is used when pulling: we want to update only the synced fields
// while preserving other fields in the local file
func MergeJSON(original, filtered []byte, filter *config.FilterConfig) ([]byte, error) {
	if filter == nil {
		return filtered, nil
	}

	var origObj map[string]interface{}
	if err := json.Unmarshal(original, &origObj); err != nil {
		// If original is empty or invalid, just return filtered
		origObj = make(map[string]interface{})
	}

	var filteredObj map[string]interface{}
	if err := json.Unmarshal(filtered, &filteredObj); err != nil {
		return nil, fmt.Errorf("failed to parse filtered JSON: %w", err)
	}

	// Merge filtered fields into original
	for key, value := range filteredObj {
		origObj[key] = value
	}

	result, err := json.MarshalIndent(origObj, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged JSON: %w", err)
	}

	return result, nil
}

// ExtractFields extracts specific fields from JSON
func ExtractFields(data []byte, fields []string) (map[string]interface{}, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	result := make(map[string]interface{})
	for _, field := range fields {
		if value, ok := obj[field]; ok {
			result[field] = value
		}
	}

	return result, nil
}

// CompareFiltered compares two JSON objects considering only the filtered fields
// Returns true if they are equal within the filter scope
func CompareFiltered(data1, data2 []byte, filter *config.FilterConfig) (bool, error) {
	filtered1, err := FilterJSON(data1, filter)
	if err != nil {
		return false, err
	}

	filtered2, err := FilterJSON(data2, filter)
	if err != nil {
		return false, err
	}

	// Normalize both by re-parsing and re-marshaling
	var obj1, obj2 map[string]interface{}
	if err := json.Unmarshal(filtered1, &obj1); err != nil {
		return false, err
	}
	if err := json.Unmarshal(filtered2, &obj2); err != nil {
		return false, err
	}

	norm1, _ := json.Marshal(obj1)
	norm2, _ := json.Marshal(obj2)

	return string(norm1) == string(norm2), nil
}
