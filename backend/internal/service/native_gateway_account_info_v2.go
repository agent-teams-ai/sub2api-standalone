package service

import "encoding/json"

const GatewayAccountInfoV2Contract = "account-info/v2"

// A finite source budget, shared by the durable writer and cached reader.
// Sources are independent observations, not a second provider facts store.
const GatewayAccountInfoMaxSources = 7

// V1 intentionally cannot express training usage as a broad privacy promise.
// Every v1 response is projected from the same canonical observation as v2.
type GatewayAccountInfoFieldsV1 struct {
	Email               GatewayAccountFact[string]                     `json:"email"`
	AccountLabel        GatewayAccountFact[string]                     `json:"accountLabel"`
	Subscription        GatewayAccountFact[GatewayAccountSubscription] `json:"subscription"`
	CredentialExpiresAt GatewayAccountFact[string]                     `json:"credentialExpiresAt"`
	PrivacyMode         GatewayAccountFact[string]                     `json:"privacyMode"`
}

type GatewayAccountInfoV1 struct {
	Status       string                      `json:"status"`
	InfoRevision string                      `json:"infoRevision,omitempty"`
	Fields       *GatewayAccountInfoFieldsV1 `json:"fields,omitempty"`
}

type GatewayAccountInfoResultV1 struct {
	Info               GatewayAccountInfoV1 `json:"info"`
	PhysicalGeneration string               `json:"physicalGeneration,omitempty"`
	PrincipalVersion   string               `json:"principalVersion,omitempty"`
}

func ProjectGatewayAccountInfoV1(result GatewayAccountInfoResult) GatewayAccountInfoResultV1 {
	out := GatewayAccountInfoResultV1{
		Info:               GatewayAccountInfoV1{Status: result.Info.Status, InfoRevision: result.Info.InfoRevision},
		PhysicalGeneration: result.PhysicalGeneration, PrincipalVersion: result.PrincipalVersion,
	}
	if fields := result.Info.Fields; fields != nil {
		out.Info.Fields = &GatewayAccountInfoFieldsV1{
			Email: fields.Email, AccountLabel: fields.AccountLabel, Subscription: fields.Subscription,
			CredentialExpiresAt: fields.CredentialExpiresAt,
			PrivacyMode:         GatewayAccountFact[string]{Status: "unsupported"},
		}
	}
	return out
}

// Stored v1 observations retain their original order, principal and timestamps.
// Only the exact old field shape is migrated, with no provider fetch or inferred
// privacy fact. A corrupt/unknown encoding fails closed instead of resetting it.
func (fields *GatewayAccountInfoFields) UnmarshalJSON(raw []byte) error {
	members, err := gatewayOAuthJSON(raw)
	if err != nil || len(members) != 5 {
		return ErrGatewayNativeIdentity
	}
	for _, key := range []string{"email", "accountLabel", "subscription", "credentialExpiresAt"} {
		if _, present := members[key]; !present {
			return ErrGatewayNativeIdentity
		}
	}
	type canonical GatewayAccountInfoFields
	var decoded canonical
	if _, current := members["trainingUsage"]; current {
		if json.Unmarshal(raw, &decoded) != nil {
			return ErrGatewayNativeIdentity
		}
	} else if _, legacy := members["privacyMode"]; legacy {
		var old GatewayAccountInfoFieldsV1
		if json.Unmarshal(raw, &old) != nil || old.PrivacyMode.Status == "known" || !gatewayInfoFactValid(old.PrivacyMode, func(value string) bool {
			return value == "private" || value == "public"
		}) {
			return ErrGatewayNativeIdentity
		}
		decoded = canonical{
			Email: old.Email, AccountLabel: old.AccountLabel, Subscription: old.Subscription,
			CredentialExpiresAt: old.CredentialExpiresAt,
			TrainingUsage:       GatewayAccountFact[string]{Status: "unsupported"},
		}
	} else {
		return ErrGatewayNativeIdentity
	}
	if !GatewayAccountInfoFieldsValid(GatewayAccountInfoFields(decoded)) {
		return ErrGatewayNativeIdentity
	}
	*fields = GatewayAccountInfoFields(decoded)
	return nil
}
