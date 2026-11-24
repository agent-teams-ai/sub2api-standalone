package service

import (
	"context"
	"encoding/json"
	"regexp"
	"sync/atomic"
)

// GatewayNativeUsage is a closed projection of provider usage at the native
// request seam. It is evidence, not a billing ledger or cost estimate.
type GatewayNativeUsage struct {
	Source                string                     `json:"source"`
	State                 string                     `json:"state"`
	InputTokens           *int64                     `json:"inputTokens,omitempty"`
	OutputTokens          *int64                     `json:"outputTokens,omitempty"`
	CachedInputTokens     *int64                     `json:"cachedInputTokens,omitempty"`
	ReasoningOutputTokens *int64                     `json:"reasoningOutputTokens,omitempty"`
	CostState             string                     `json:"costState"`
	Finalized             bool                       `json:"finalized"`
	NativeContext         *GatewayNativeUsageContext `json:"nativeContext,omitempty"`
}

// Native context is captured once by authenticated connection dispatch. These
// identifiers describe that admitted request, never a later catalog read.
type GatewayNativeUsageContext struct {
	ProviderID string                    `json:"providerId"`
	ProductID  string                    `json:"productId"`
	ProfileID  string                    `json:"profileId"`
	ModelID    string                    `json:"modelId"`
	Billing    GatewayNativeUsageBilling `json:"billing"`
}

// A selected credential policy does not establish an observed billing kind.
// The connection seam currently has no such observation and emits unknown.
type GatewayNativeUsageBilling struct {
	Status string `json:"status"`
}

type gatewayNativeUsageObserverKey struct{}

// WithGatewayNativeUsageObserver is wired by the trusted private transport only.
// Observations never carry payload, upstream account identifiers, headers or
// credentials. Legacy observers retain their original scalar wire projection.
func WithGatewayNativeUsageObserver(ctx context.Context, observe func(GatewayNativeUsage)) context.Context {
	return context.WithValue(ctx, gatewayNativeUsageObserverKey{}, observe)
}

func withGatewayNativeConnectionUsage(ctx context.Context, publication GatewayConnectionPublication, model string, entered *atomic.Bool) (context.Context, func(), error) {
	noop := func() {}
	definition, found := LookupGatewayConnectionProfile(publication.Connection.ProfileID)
	if ctx == nil || entered == nil || !GatewayConnectionPublicationValid(publication) || !found ||
		definition.DispatchFamily != publication.DispatchFamily || !gatewayConnectionSemanticID.MatchString(definition.Provider.ID) ||
		!gatewayConnectionSemanticID.MatchString(definition.Product.ID) || !GatewayAccountModelIDValid(model) || len(model) > 128 {
		return nil, noop, ErrGatewayNativeIdentity
	}
	observe, observed := ctx.Value(gatewayNativeUsageObserverKey{}).(func(GatewayNativeUsage))
	if !observed || observe == nil {
		return ctx, noop, nil
	}
	// This lookup belongs to the native source at authenticated admission. Only
	// value copies survive; receipt/readback must never re-project a current
	// definition or infer historical billing from a profile or credential kind.
	frozen := GatewayNativeUsageContext{ProviderID: definition.Provider.ID, ProductID: definition.Product.ID,
		ProfileID: publication.Connection.ProfileID, ModelID: model, Billing: GatewayNativeUsageBilling{Status: "unknown"}}
	var observedUsage atomic.Bool
	publish := func(usage GatewayNativeUsage) {
		observedUsage.Store(true)
		usage.NativeContext = nil
		if entered.Load() {
			copy := frozen
			usage.NativeContext = &copy
		}
		observe(usage)
	}
	finish := func() {
		// Some protocols fail on HTTP status or transport before starting their
		// usage parser. The admitted source remains known, but its counters do
		// not: preserve that fact without inferring zero or replaying anything.
		if entered.Load() && observedUsage.CompareAndSwap(false, true) {
			publish(GatewayNativeUsage{Source: "native_provider_response", State: "unknown", CostState: "unknown", Finalized: true})
		}
	}
	return WithGatewayNativeUsageObserver(ctx, publish), finish, nil
}

var gatewayNativeUsageInteger = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func gatewayNativeUsageCount(raw json.RawMessage) *int64 {
	var count int64
	if !gatewayNativeUsageInteger.Match(raw) || json.Unmarshal(raw, &count) != nil || count > 9007199254740991 {
		return nil
	}
	return &count
}

// Reject ambiguous protocol keys rather than deriving numbers from tool/user data.
func gatewayNativeObserveUsage(output []byte, observation *GatewayNativeUsage, complete bool) {
	fields, ok := gatewayNativeCanonicalObject(output, "usage")
	if !ok || fields["usage"] == nil {
		return
	}
	usage, ok := gatewayNativeCanonicalObject(fields["usage"], "input_tokens", "output_tokens", "input_tokens_details", "output_tokens_details")
	if !ok {
		return
	}
	input, out := gatewayNativeUsageCount(usage["input_tokens"]), gatewayNativeUsageCount(usage["output_tokens"])
	if input == nil && out == nil {
		return
	}
	observation.InputTokens, observation.OutputTokens = input, out
	observation.CachedInputTokens, observation.ReasoningOutputTokens = nil, nil
	if details, valid := gatewayNativeCanonicalObject(usage["input_tokens_details"], "cached_tokens"); valid {
		n := gatewayNativeUsageCount(details["cached_tokens"])
		if n != nil && input != nil && *n <= *input {
			observation.CachedInputTokens = n
		}
	}
	if details, valid := gatewayNativeCanonicalObject(usage["output_tokens_details"], "reasoning_tokens"); valid {
		n := gatewayNativeUsageCount(details["reasoning_tokens"])
		if n != nil && out != nil && *n <= *out {
			observation.ReasoningOutputTokens = n
		}
	}
	observation.State = "partial"
	if complete && input != nil && out != nil {
		observation.State = "complete"
	}
}

// Chat usage keeps absent counters absent; the typed compatibility usage has
// zero-valued integers and cannot establish native accounting completeness.
func gatewayNativeObserveChatUsage(output []byte, observation *GatewayNativeUsage) bool {
	fields, ok := gatewayNativeCanonicalObject(output, "usage")
	if !ok || fields["usage"] == nil {
		return false
	}
	usage, ok := gatewayNativeCanonicalObject(fields["usage"], "prompt_tokens", "completion_tokens", "prompt_tokens_details", "completion_tokens_details")
	if !ok {
		return false
	}
	normalized, err := json.Marshal(map[string]any{"usage": map[string]json.RawMessage{
		"input_tokens": usage["prompt_tokens"], "output_tokens": usage["completion_tokens"],
		"input_tokens_details": usage["prompt_tokens_details"], "output_tokens_details": usage["completion_tokens_details"],
	}})
	if err == nil {
		gatewayNativeObserveUsage(normalized, observation, false)
	}
	return err == nil && gatewayNativeUsageCount(usage["prompt_tokens"]) != nil && gatewayNativeUsageCount(usage["completion_tokens"]) != nil
}
