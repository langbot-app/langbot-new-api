package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MCP tool schemas (e.g. Bright Data search_dataset.search_after) declare bare
// arrays; Gemini rejects any ARRAY without items ("items: missing field").
func TestCleanFunctionParametersAddsItemsToBareArrays(t *testing.T) {
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"search_after": map[string]interface{}{"type": "array", "description": "cursor"},
			"tags":         map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"nested": map[string]interface{}{
				"anyOf": []interface{}{
					map[string]interface{}{"type": "array"},
					map[string]interface{}{"$ref": "#/properties/tags"},
				},
			},
		},
		"additionalProperties": false,
		"$schema":              "http://json-schema.org/draft-07/schema#",
	}

	cleaned, ok := CleanFunctionParameters(params).(map[string]interface{})
	require.True(t, ok)
	props := cleaned["properties"].(map[string]interface{})

	assert.Equal(t, map[string]interface{}{"type": "ARRAY", "description": "cursor", "items": map[string]interface{}{}}, props["search_after"])
	assert.Equal(t, map[string]interface{}{"type": "ARRAY", "items": map[string]interface{}{"type": "STRING"}}, props["tags"])
	nested := props["nested"].(map[string]interface{})["anyOf"].([]interface{})
	assert.Equal(t, map[string]interface{}{"type": "ARRAY", "items": map[string]interface{}{}}, nested[0])
	assert.NotContains(t, cleaned, "additionalProperties")
	assert.NotContains(t, cleaned, "$schema")
}
