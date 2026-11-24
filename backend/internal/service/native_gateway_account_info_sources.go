package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/tidwall/gjson"
)

func gatewayInfoSubscription(code, scope, expiry string) *GatewayAccountSubscription {
	code = strings.TrimSpace(code)
	if !gatewayInfoText(code, 100) {
		return nil
	}
	label := openai.PlanTypeLabel(code, false)
	if label == "" {
		label = code
	}
	v := &GatewayAccountSubscription{Code: code, Label: label, Scope: scope}
	if expiry != "" {
		if at, err := time.Parse(time.RFC3339Nano, expiry); err == nil {
			// Normalize before validation: an otherwise positive local year can
			// cross into year zero (or 10000) when its offset becomes UTC.
			value := at.UTC().Format(time.RFC3339Nano)
			if gatewayInfoInstant(value) {
				v.EndsAt = &GatewayAccountInfoDate{Kind: "instant", Value: value}
			}
		} else if date, err := time.Parse("2006-01-02", expiry); err == nil && date.Year() >= 1 && date.Format("2006-01-02") == expiry {
			v.EndsAt = &GatewayAccountInfoDate{Kind: "date", Value: expiry}
		}
	}
	return v
}

// Called only after the existing fixed issuer/audience/signature verifier
// accepts these exact claim bytes. Imported JSON never calls this producer.
func gatewayVerifiedCodexInfo(claims map[string]json.RawMessage, at time.Time) GatewayAccountInfoFields {
	f := GatewayAccountInfoEmptyFields(GatewayCodexOAuthResponsesProfile)
	if email, ok := gatewayOAuthString(claims, "email"); ok && gatewayInfoText(email, 320) {
		f.Email = gatewayKnownFact(email, "verified_claim", at)
	}
	if label, ok := gatewayOAuthString(claims, "name"); ok && gatewayInfoText(label, 200) {
		f.AccountLabel = gatewayKnownFact(label, "verified_claim", at)
	}
	if auth, err := gatewayOAuthJSON(claims["https://api.openai.com/auth"]); err == nil {
		if plan, ok := gatewayOAuthString(auth, "chatgpt_plan_type"); ok {
			if v := gatewayInfoSubscription(plan, "unknown", ""); v != nil {
				f.Subscription = gatewayKnownFact(*v, "verified_claim", at)
			}
		}
	}
	return f
}

// Account selection is already strict and principal-bound. Pure existing plan
// extraction is reused, while privacy mutation and extra HTTP are excluded.
func gatewayCodexAccountInfo(raw []byte, selected string, at time.Time) GatewayAccountInfoFields {
	f := GatewayAccountInfoEmptyFields(GatewayCodexOAuthResponsesProfile)
	var root struct {
		Accounts map[string]map[string]any `json:"accounts"`
	}
	if json.Unmarshal(raw, &root) != nil {
		return f
	}
	for _, record := range root.Accounts {
		account, ok := record["account"].(map[string]any)
		if !ok || account["account_id"] != selected {
			continue
		}
		scope := "unknown"
		if kind, ok := account["account_structure"].(string); ok {
			switch kind {
			case "personal", "organization", "workspace":
				scope = kind
			}
		}
		if label, ok := account["name"].(string); ok && gatewayInfoText(label, 200) {
			f.AccountLabel = gatewayKnownFact(label, "provider_api", at)
		}
		if v := gatewayInfoSubscription(extractPlanType(record), scope, extractEntitlementExpiresAt(record)); v != nil {
			f.Subscription = gatewayKnownFact(*v, "provider_api", at)
		}
		return f
	}
	return f
}

// The recognized successful quota envelope is checked by the quota parser
// first. Absence in this complete source is a deliberate subscription removal.
func gatewayQuotaAccountInfo(provider string, body []byte, observed time.Time) GatewayAccountInfoFields {
	profile, path := "", ""
	switch provider {
	case "glm":
		profile, path = GatewayGLMCodingResponsesProfile, "data.level"
	case "minimax":
		profile, path = GatewayMiniMaxMPlanResponsesProfile, "current_subscribe_title"
	default:
		return GatewayAccountInfoFields{}
	}
	f := GatewayAccountInfoEmptyFields(profile)
	v := gjson.GetBytes(body, path)
	if v.Type == gjson.String {
		code := strings.TrimSpace(v.String())
		if gatewayInfoText(code, 100) {
			f.Subscription = gatewayKnownFact(GatewayAccountSubscription{Code: code, Label: code, Scope: "unknown"}, "provider_api", observed)
		}
	}
	return f
}

// The same native billing normalizer drives the admin and protected views.
// A monthly limit is evidence for a heuristic, not an asserted provider plan;
// billing period end is deliberately not relabeled as subscription expiry.
func gatewayGrokBillingAccountInfo(body []byte, observed time.Time) *GatewayAccountInfoFields {
	payload, err := xai.ParseBillingPayload(body)
	if err != nil || payload.Config == nil {
		return nil
	}
	return gatewayGrokBillingSummaryInfo(xai.BuildBillingSummary(payload.Config), observed)
}

// Accept only a summary produced from the current authenticated billing reply;
// callers must not pass merged/cache/imported extras here.
func gatewayGrokBillingSummaryInfo(summary *xai.BillingSummary, observed time.Time) *GatewayAccountInfoFields {
	if summary == nil || observed.IsZero() {
		return nil
	}
	fields := GatewayAccountInfoEmptyFields(GatewayGrokOAuthResponsesProfile)
	plan := xai.CanonicalGrokPlan(summary.MonthlyLimitCents, "", nil)
	if gatewayInfoText(plan, 100) {
		label := summary.Plan
		if !gatewayInfoText(label, 160) {
			label = plan
		}
		fields.Subscription = gatewayKnownFact(GatewayAccountSubscription{Code: plan, Label: label, Scope: "unknown"}, "inferred", observed)
	}
	return &fields
}

// Only the current authenticated LoadCodeAssist response enters this producer.
// An omitted tier is unknown; the legacy nil/empty -> Free default is not evidence.
func gatewayAntigravityAccountInfo(response *antigravity.LoadCodeAssistResponse, observed time.Time) *GatewayAccountInfoFields {
	if response == nil || observed.IsZero() {
		return nil
	}
	fields := GatewayAccountInfoEmptyFields("antigravity-responses-oauth-v1")
	tier := strings.TrimSpace(response.GetTier())
	if gatewayInfoText(tier, 100) {
		label := NormalizeAntigravitySubscription(response).PlanType
		if !gatewayInfoText(label, 160) {
			label = tier
		}
		fields.Subscription = gatewayKnownFact(GatewayAccountSubscription{Code: tier, Label: label, Scope: "unknown"}, "provider_api", observed)
	}
	return &fields
}

// Called only by native auth/refresh after a provider-confirmed privacy action.
// Imported account extras and arbitrary user fields never enter this producer.
func GatewayConfirmedTrainingInfo(profile, nativeMode string, observed time.Time) *GatewayAccountInfoFields {
	if !gatewayInfoSourceAllowed(profile, "training_settings") || nativeMode != PrivacyModeTrainingOff || observed.IsZero() {
		return nil
	}
	fields := GatewayAccountInfoEmptyFields(profile)
	fields.TrainingUsage = gatewayKnownFact("disabled", "provider_api", observed)
	return &fields
}

// A chosen Google tier is declared context, not a verified subscription claim.
func GatewayDeclaredSubscriptionInfo(profile, tier string, observed time.Time) *GatewayAccountInfoFields {
	if !gatewayInfoSourceAllowed(profile, "user_declaration") || !gatewayInfoText(tier, 100) || observed.IsZero() {
		return nil
	}
	fields := GatewayAccountInfoEmptyFields(profile)
	fields.Subscription = gatewayKnownFact(GatewayAccountSubscription{Code: tier, Label: tier, Scope: "unknown"}, "user_supplied", observed)
	return &fields
}

func gatewayCodexCredentialInfo(bundle GatewayNativeOAuthBundle, custody *GatewayNativeCredentialCustody, at time.Time) GatewayAccountInfoFields {
	f := GatewayAccountInfoEmptyFields(GatewayCodexOAuthResponsesProfile)
	if _, valid := gatewayOAuthBundleDue(bundle, at, custody); !valid {
		return f
	}
	fields, err := gatewayOAuthJSON(bundle.SensitiveMetadata)
	if err != nil {
		return f
	}
	var timing gatewayOAuthTiming
	if json.Unmarshal(fields[gatewayOAuthTimingMember], &timing) == nil {
		expiry, e1 := time.Parse(time.RFC3339Nano, timing.AccessExpires)
		observed, e2 := time.Parse(time.RFC3339Nano, timing.RequestStarted)
		if e1 == nil && e2 == nil {
			return gatewayOAuthCredentialExpiryInfo(GatewayCodexOAuthResponsesProfile, expiry, observed)
		}
	}
	return f
}

// An expiry is observable only when its existing authenticated producer kept
// the observation time. Legacy bundles without that time remain unknown.
func gatewayOAuthCredentialExpiryInfo(profile string, expiry, observed time.Time) GatewayAccountInfoFields {
	f := GatewayAccountInfoEmptyFields(profile)
	value := expiry.UTC().Format(time.RFC3339Nano)
	if !expiry.IsZero() && !observed.IsZero() && gatewayInfoInstant(value) && gatewayInfoInstant(observed.UTC().Format(time.RFC3339Nano)) {
		f.CredentialExpiresAt = gatewayKnownFact(value, "provider_api", observed)
	}
	return f
}
