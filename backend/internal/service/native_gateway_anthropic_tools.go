package service

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// Protected native paths preserve the declared object schema and the native
// strict flag. The ordinary compatibility converter may normalize schemas and
// omits strict; neither transformation may silently weaken this boundary.
// Client custom/namespace tools have already been reversibly lowered before
// this function, and their original identities are restored on Responses SSE.
func gatewayNativeMarshalAnthropicRequest(request *apicompat.AnthropicRequest, tools []apicompat.ResponsesTool) ([]byte, error) {
	if request == nil || len(request.Tools) != len(tools) {
		return nil, ErrGatewayNativeIdentity
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	if len(tools) == 0 {
		return raw, nil
	}
	fields, valid := gatewayNativeCanonicalObject(raw)
	if !valid {
		return nil, ErrGatewayNativeIdentity
	}
	declared := make([]map[string]any, 0, len(tools))
	for index, tool := range tools {
		schema, valid := gatewayNativeCanonicalObject(tool.Parameters, "type")
		var kind string
		if !valid || tool.Type != "function" || tool.Name == "" || request.Tools[index].Name != tool.Name || request.Tools[index].Type != "" ||
			json.Unmarshal(schema["type"], &kind) != nil || kind != "object" || schema["oneOf"] != nil || schema["anyOf"] != nil || schema["allOf"] != nil {
			return nil, ErrGatewayNativeIdentity
		}
		wire := map[string]any{"name": tool.Name, "input_schema": tool.Parameters}
		if tool.Description != "" {
			wire["description"] = tool.Description
		}
		if tool.Strict != nil {
			wire["strict"] = *tool.Strict
		}
		declared = append(declared, wire)
	}
	fields["tools"], err = json.Marshal(declared)
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	return json.Marshal(fields)
}
