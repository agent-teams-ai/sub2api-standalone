package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// CodexSessionCredentialInput is transient input shared by native admin import
// and owner-scoped connection import. Identity fields are unverified hints;
// parsing this document never grants an account identity or metadata fact.
type CodexSessionCredentialInput struct {
	AccessToken, RefreshToken, IDToken           string
	AgentRuntimeID, AgentPrivateKey, AgentTaskID string
	AccountID, UserID, Email, PlanType           string
	AgentFedRAMP, IsAgentIdentity                bool
	TokenExpiresAt                               *time.Time
}

func codexSessionValue(raw map[string]any, path []string) (any, bool) {
	var value any = raw
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[key]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

func codexSessionString(raw map[string]any, paths ...[]string) string {
	for _, path := range paths {
		value, _ := codexSessionValue(raw, path)
		var text string
		switch v := value.(type) {
		case string:
			text = v
		case json.Number:
			text = v.String()
		case float64:
			text = strconv.FormatFloat(v, 'f', -1, 64)
		case float32:
			text = strconv.FormatFloat(float64(v), 'f', -1, 32)
		case int:
			text = strconv.Itoa(v)
		case int64:
			text = strconv.FormatInt(v, 10)
		case int32:
			text = strconv.FormatInt(int64(v), 10)
		}
		if text = strings.TrimSpace(text); text != "" {
			return text
		}
	}
	return ""
}

func codexSessionBool(raw map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch value := raw[key].(type) {
		case bool:
			return value
		case string:
			if parsed, err := strconv.ParseBool(strings.TrimSpace(value)); err == nil {
				return parsed
			}
		}
	}
	return false
}

// ParseCodexSessionCredentialInput keeps the existing native aliases in one
// place. Container selection, size limits, trusted provenance and persistence
// belong to the caller; no global admin account lookup is performed here.
func ParseCodexSessionCredentialInput(value any) (CodexSessionCredentialInput, error) {
	var out CodexSessionCredentialInput
	switch raw := value.(type) {
	case string:
		out.AccessToken = strings.TrimSpace(raw)
	case map[string]any:
		agent, found := raw["agent_identity"].(map[string]any)
		if !found {
			agent, found = raw["agentIdentity"].(map[string]any)
		}
		if found || strings.EqualFold(codexSessionString(raw, []string{"auth_mode"}, []string{"authMode"}), OpenAIAuthModeAgentIdentity) {
			if !found {
				agent = raw
			}
			out.IsAgentIdentity = true
			out.AgentRuntimeID = codexSessionString(agent, []string{"agent_runtime_id"}, []string{"agentRuntimeId"})
			out.AgentPrivateKey = codexSessionString(agent, []string{"agent_private_key"}, []string{"agentPrivateKey"})
			out.AgentTaskID = codexSessionString(agent, []string{"task_id"}, []string{"taskId"})
			out.AccountID = codexSessionString(agent, []string{"account_id"}, []string{"accountId"})
			out.UserID = codexSessionString(agent, []string{"chatgpt_user_id"}, []string{"chatgptUserId"})
			out.Email = codexSessionString(agent, []string{"email"})
			out.PlanType = codexSessionString(agent, []string{"plan_type"}, []string{"planType"})
			out.AgentFedRAMP = codexSessionBool(agent, "chatgpt_account_is_fedramp", "chatgptAccountIsFedramp")
			if out.AgentRuntimeID == "" || out.AgentPrivateKey == "" || out.AccountID == "" || out.UserID == "" {
				return CodexSessionCredentialInput{}, errors.New("agent identity 缺少必要字段")
			}
			if ValidateOpenAIAgentIdentityPrivateKey(out.AgentPrivateKey) != nil {
				return CodexSessionCredentialInput{}, errors.New("agent identity private key 格式无效")
			}
			return out, nil
		}
		out.AccessToken = codexSessionString(raw, []string{"tokens", "access_token"}, []string{"tokens", "accessToken"}, []string{"access_token"}, []string{"accessToken"}, []string{"token"})
		out.RefreshToken = codexSessionString(raw, []string{"tokens", "refresh_token"}, []string{"tokens", "refreshToken"}, []string{"refresh_token"}, []string{"refreshToken"})
		out.IDToken = codexSessionString(raw, []string{"tokens", "id_token"}, []string{"tokens", "idToken"}, []string{"id_token"}, []string{"idToken"})
		for _, path := range [][]string{{"tokens", "expires_at"}, {"tokens", "expiresAt"}, {"expires_at"}, {"expiresAt"}} {
			value, found := codexSessionValue(raw, path)
			if expiry, valid := ParseCodexCredentialTime(value); found && valid {
				out.TokenExpiresAt = &expiry
				break
			}
		}
	default:
		return out, errors.New("缺少 accessToken/access_token")
	}
	if out.AccessToken == "" {
		return out, errors.New("缺少 accessToken/access_token")
	}
	return out, nil
}
