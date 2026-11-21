//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The Action's original schema is the independent source oracle. Anthropic's
// documented grammar cannot represent minimum/maximum, so native code moves
// exactly these constraints to descriptions and validates the final response
// against the original schema. Compare its entire provider wire to this
// independently derived expectation, never to a Native helper or response.
// https://platform.claude.com/docs/en/build-with-claude/structured-outputs
func nativeConnectionJoinedAnthropicSchemaDigest(original json.RawMessage) string {
	if len(original) == 0 || len(original) > 64<<10 {
		return ""
	}
	d := json.NewDecoder(bytes.NewReader(original))
	d.UseNumber()
	var schema map[string]any
	if d.Decode(&schema) != nil || d.Decode(new(any)) != io.EOF || schema == nil {
		return ""
	}
	nodes := 0
	var visit func(map[string]any, int) bool
	visit = func(node map[string]any, depth int) bool {
		nodes++
		if depth > 16 || nodes > 2048 || node == nil {
			return false
		}
		for name, value := range node {
			switch name {
			case "properties":
				properties, ok := value.(map[string]any)
				if !ok {
					return false
				}
				for _, child := range properties {
					object, ok := child.(map[string]any)
					if !ok || !visit(object, depth+1) {
						return false
					}
				}
			case "items":
				object, ok := value.(map[string]any)
				if !ok || !visit(object, depth+1) {
					return false
				}
			case "anyOf":
				children, ok := value.([]any)
				if !ok || len(children) == 0 || len(children) > 16 {
					return false
				}
				for _, child := range children {
					object, ok := child.(map[string]any)
					if !ok || !visit(object, depth+1) {
						return false
					}
				}
			}
		}
		parts := []string{}
		if description, exists := node["description"]; exists {
			text, ok := description.(string)
			if !ok {
				return false
			}
			if text != "" {
				parts = append(parts, text)
			}
		}
		changed := false
		for _, bound := range []string{"minimum", "maximum"} {
			value, exists := node[bound]
			if !exists {
				continue
			}
			number, ok := value.(json.Number)
			if !ok {
				return false
			}
			relation := "greater than or equal to "
			if bound == "maximum" {
				relation = "less than or equal to "
			}
			parts = append(parts, "Numeric values must be "+relation+number.String()+".")
			delete(node, bound)
			changed = true
		}
		if changed {
			node["description"] = strings.Join(parts, " ")
		}
		return true
	}
	if !visit(schema, 0) {
		return ""
	}
	wire, err := json.Marshal(schema)
	if err != nil {
		return ""
	}
	return nativeConnectionJoinedSchemaDigest(wire)
}

func TestNativeConnectionJoinedSchemaOracleRetainsConstraints(t *testing.T) {
	original := json.RawMessage(`{"type":"object","properties":{"findings":{"type":"array","items":{"type":"object","properties":{"confidence":{"type":"number","description":"Source confidence.","minimum":0,"maximum":1},"minimum":{"type":"string","enum":["maximum"]}},"required":["confidence","minimum"],"additionalProperties":false}},"revalidations":{"anyOf":[{"type":"null"},{"type":"number","minimum":0}]}},"required":["findings","revalidations"],"additionalProperties":false}`)
	expected := json.RawMessage(`{"type":"object","properties":{"findings":{"type":"array","items":{"type":"object","properties":{"confidence":{"type":"number","description":"Source confidence. Numeric values must be greater than or equal to 0. Numeric values must be less than or equal to 1."},"minimum":{"type":"string","enum":["maximum"]}},"required":["confidence","minimum"],"additionalProperties":false}},"revalidations":{"anyOf":[{"type":"null"},{"type":"number","description":"Numeric values must be greater than or equal to 0."}]}},"required":["findings","revalidations"],"additionalProperties":false}`)
	digest := nativeConnectionJoinedAnthropicSchemaDigest(original)
	require.Equal(t, nativeConnectionJoinedSchemaDigest(expected), digest)
	require.NotEqual(t, nativeConnectionJoinedSchemaDigest(original), digest)
	require.Contains(t, string(original), `"maximum":1`, "oracle cannot mutate the pinned input")
	for _, broken := range []string{
		strings.Replace(string(expected), "less than or equal to 1.", "less than or equal to 2.", 1),
		strings.Replace(string(expected), `"required":["confidence","minimum"]`, `"required":["confidence"]`, 1),
		strings.Replace(string(expected), `"additionalProperties":false`, `"additionalProperties":true`, 1),
		strings.Replace(string(expected), `"enum":["maximum"]`, `"enum":["minimum"]`, 1),
	} {
		require.NotEqual(t, digest, nativeConnectionJoinedSchemaDigest(json.RawMessage(broken)), "semantic loss is not a grammar lowering")
	}
}
