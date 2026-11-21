package antigravity

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// WrapGenerateContent preserves already validated native content/signatures
// and schemas. It adds the same native identity, session and routing envelope
// as TransformClaudeToGemini without its dummy-signature or history downgrade
// behavior, which is not valid for an authenticated private continuation.
func WrapGenerateContent(raw []byte, project, model string) ([]byte, error) {
	var request map[string]json.RawMessage
	if len(raw) > 4<<20 || json.Unmarshal(raw, &request) != nil || project == "" || model == "" {
		return nil, errors.New("invalid antigravity GenerateContent request")
	}
	var contents []GeminiContent
	if json.Unmarshal(request["contents"], &contents) != nil || len(contents) == 0 {
		return nil, errors.New("invalid antigravity GenerateContent contents")
	}
	var system GeminiContent
	if request["systemInstruction"] != nil && json.Unmarshal(request["systemInstruction"], &system) != nil {
		return nil, errors.New("invalid antigravity GenerateContent system")
	}
	blocks := make([]SystemBlock, 0, len(system.Parts))
	for _, part := range system.Parts {
		blocks = append(blocks, SystemBlock{Type: "text", Text: part.Text})
	}
	systemInput, err := json.Marshal(blocks)
	if err != nil {
		return nil, err
	}
	request["systemInstruction"], err = json.Marshal(buildSystemInstruction(systemInput, model, DefaultTransformOptions(), nil))
	if err != nil {
		return nil, err
	}
	request["sessionId"], err = json.Marshal(generateStableSessionID(contents))
	if err != nil {
		return nil, err
	}
	if request["toolConfig"] == nil {
		request["toolConfig"] = json.RawMessage(`{"functionCallingConfig":{"mode":"VALIDATED"}}`)
	}
	return json.Marshal(struct {
		Project     string                     `json:"project"`
		RequestID   string                     `json:"requestId"`
		UserAgent   string                     `json:"userAgent"`
		RequestType string                     `json:"requestType"`
		Model       string                     `json:"model"`
		Request     map[string]json.RawMessage `json:"request"`
	}{project, "agent-" + uuid.NewString(), "antigravity", "agent", model, request})
}
