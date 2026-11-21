package service

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	GatewayAccountCapabilitiesContractV2 = "native-account-capabilities/v2"
	GatewayConnectionInputBytes          = 64 * 1024
	GatewayConnectionInputFields         = 16
)

// GatewayConnectionSelection is the immutable semantic selection accepted
// before provider entry. Labels, model freshness and unrelated catalog changes
// are deliberately absent. A selected method does not predict the credential
// variant that native parsing will accept.
type GatewayConnectionSelection struct {
	ProfileID               string `json:"profileId"`
	MethodID                string `json:"methodId"`
	MethodRevision          int    `json:"methodRevision"`
	ConfigurationGeneration string `json:"configurationGeneration"`
}

// GatewayCredentialAttestation is issued only after native validation. Native
// owns structured credentials/signing; "native" never means a bearer fallback.
// RefreshPolicy describes the accepted credential, not the connection form.
type GatewayCredentialAttestation struct {
	CredentialKind   string `json:"credentialKind"`
	CredentialPolicy string `json:"credentialPolicy"`
	RefreshPolicy    string `json:"refreshPolicy"`
}

type GatewayConnectionFieldChoice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type GatewayConnectionField struct {
	FieldID  string                         `json:"fieldId"`
	Kind     string                         `json:"kind"`
	Label    string                         `json:"label"`
	Required bool                           `json:"required"`
	MaxBytes int                            `json:"maxBytes"`
	Help     string                         `json:"help,omitempty"`
	Choices  []GatewayConnectionFieldChoice `json:"choices,omitempty"`
}

type GatewayConnectionInteraction struct {
	Kind                string                   `json:"kind"`
	PreflightFields     []GatewayConnectionField `json:"preflightFields,omitempty"`
	InputFields         []GatewayConnectionField `json:"inputFields,omitempty"`
	CallbackMode        string                   `json:"callbackMode,omitempty"`
	CompletionInputKind string                   `json:"completionInputKind,omitempty"`
}

type GatewayConnectionMethod struct {
	MethodID                  string                       `json:"methodId"`
	Revision                  int                          `json:"revision"`
	ConfigurationGeneration   string                       `json:"configurationGeneration"`
	Label                     string                       `json:"label"`
	Create                    bool                         `json:"create"`
	Reconnect                 bool                         `json:"reconnect"`
	AllowedCredentialPolicies []string                     `json:"allowedCredentialPolicies"`
	Requirements              []string                     `json:"requirements"`
	Availability              string                       `json:"availability"`
	Reason                    string                       `json:"reason,omitempty"`
	Interaction               GatewayConnectionInteraction `json:"interaction"`
}

type GatewayConnectionModelPolicy struct {
	SourceID            string `json:"sourceId"`
	Contract            string `json:"contract"`
	UnavailableFallback string `json:"unavailableFallback"`
}

// GatewayConnectionMount is private composition evidence. It is never part of
// the public provider-connections projection or accepted from credential input.
type GatewayConnectionMount struct {
	BaseURL          string `json:"baseURL"`
	Model            string `json:"model"`
	QualificationRef string `json:"qualificationRef"`
}

type GatewayConnectionLegacyAlias struct {
	Kind         string                     `json:"kind"`
	Connection   GatewayConnectionSelection `json:"connection"`
	InputFieldID string                     `json:"inputFieldId,omitempty"`
}

type GatewayConnectionCapability struct {
	ProfileID      string                         `json:"profileId"`
	Provider       GatewayAccountCapabilityLabel  `json:"provider"`
	Product        GatewayAccountCapabilityLabel  `json:"product"`
	Region         GatewayAccountCapabilityLabel  `json:"region"`
	Protocol       string                         `json:"protocol"`
	Models         []GatewayAccountModel          `json:"models"`
	ModelPolicy    GatewayConnectionModelPolicy   `json:"modelPolicy"`
	Methods        []GatewayConnectionMethod      `json:"methods"`
	AccountInfo    bool                           `json:"accountInfo"`
	LimitsContract string                         `json:"limitsContract"`
	Availability   string                         `json:"availability"`
	Mount          GatewayConnectionMount         `json:"mount"`
	LegacyAliases  []GatewayConnectionLegacyAlias `json:"legacyAliases"`
}

type GatewayAccountCapabilitiesV2 struct {
	Contract string                        `json:"contract"`
	Profiles []GatewayConnectionCapability `json:"profiles"`
}

var gatewayConnectionSemanticID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,127}$`)

var gatewayConnectionIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func GatewayConnectionSelectionValid(v GatewayConnectionSelection) bool {
	return len(v.ProfileID) <= 64 && gatewayConnectionIdentifier.MatchString(v.ProfileID) &&
		gatewayConnectionSemanticID.MatchString(v.MethodID) &&
		v.MethodRevision > 0 && v.MethodRevision <= 1_000_000 &&
		gatewayConnectionIdentifier.MatchString(v.ConfigurationGeneration)
}

func GatewayCredentialAttestationValid(v GatewayCredentialAttestation) bool {
	if v.CredentialKind != "api_key" && v.CredentialKind != "oauth" && v.CredentialKind != "native" {
		return false
	}
	return gatewayCredentialPolicyValid(v.CredentialPolicy) && (v.RefreshPolicy == "native" || v.RefreshPolicy == "none")
}

func gatewayCredentialPolicyValid(v string) bool {
	switch v {
	case "provider_key", "subscription_session", "cloud_identity", "delegated_identity":
		return true
	default:
		return false
	}
}

func gatewayConnectionRequirementSupported(v string) bool {
	return v == "owner_scoped_custody/v1" || v == "effect_fence/v1" || v == "native_dispatch/v1"
}

func gatewayConnectionTextValid(v string, limit int, empty bool) bool {
	if !utf8.ValidString(v) || len(v) > limit || (!empty && strings.TrimSpace(v) == "") {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func gatewayConnectionFieldsValid(fields []GatewayConnectionField) bool {
	if len(fields) > GatewayConnectionInputFields {
		return false
	}
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if !gatewayConnectionSemanticID.MatchString(f.FieldID) || seen[f.FieldID] ||
			!gatewayConnectionTextValid(f.Label, 128, false) || !gatewayConnectionTextValid(f.Help, 512, true) ||
			f.MaxBytes < 1 || f.MaxBytes > GatewayConnectionInputBytes {
			return false
		}
		seen[f.FieldID] = true
		if f.Kind != "text" && f.Kind != "secret" && f.Kind != "document" && f.Kind != "select" {
			return false
		}
		if f.Kind != "select" {
			if len(f.Choices) != 0 {
				return false
			}
			continue
		}
		if len(f.Choices) < 1 || len(f.Choices) > 64 {
			return false
		}
		choices := make(map[string]bool, len(f.Choices))
		for _, c := range f.Choices {
			if !gatewayConnectionTextValid(c.Value, min(f.MaxBytes, 256), false) || !gatewayConnectionTextValid(c.Label, 128, false) || choices[c.Value] {
				return false
			}
			choices[c.Value] = true
		}
	}
	return true
}

// GatewayConnectionMethodValid is the native registration/ingress boundary.
// Unknown policy/requirement fails closed for this method. Consumers separately
// retain unknown bounded descriptors as unavailable for forward compatibility.
func GatewayConnectionMethodValid(v GatewayConnectionMethod) bool {
	selection := GatewayConnectionSelection{"profile", v.MethodID, v.Revision, v.ConfigurationGeneration}
	if !GatewayConnectionSelectionValid(selection) || !gatewayConnectionTextValid(v.Label, 128, false) ||
		len(v.AllowedCredentialPolicies) < 1 || len(v.AllowedCredentialPolicies) > 4 ||
		len(v.Requirements) < 1 || len(v.Requirements) > 16 ||
		(v.Availability != "enabled" && v.Availability != "deployment_configuration_required") ||
		(v.Reason != "" && !gatewayConnectionSemanticID.MatchString(v.Reason)) {
		return false
	}
	policies, requirements := map[string]bool{}, map[string]bool{}
	for _, p := range v.AllowedCredentialPolicies {
		if !gatewayCredentialPolicyValid(p) || policies[p] {
			return false
		}
		policies[p] = true
	}
	for _, r := range v.Requirements {
		if !gatewayConnectionRequirementSupported(r) || requirements[r] {
			return false
		}
		requirements[r] = true
	}
	if !requirements["owner_scoped_custody/v1"] || !requirements["effect_fence/v1"] || !requirements["native_dispatch/v1"] {
		return false
	}
	i := v.Interaction
	if !gatewayConnectionFieldsValid(i.PreflightFields) || !gatewayConnectionFieldsValid(i.InputFields) {
		return false
	}
	switch i.Kind {
	case "browser":
		return len(i.InputFields) == 0 && (i.CallbackMode == "automatic" || i.CallbackMode == "manual") &&
			(i.CompletionInputKind == "callback_url" || i.CompletionInputKind == "authorization_code")
	case "device":
		return len(i.InputFields) == 0 && i.CallbackMode == "" && i.CompletionInputKind == ""
	case "credential_form":
		return len(i.PreflightFields) == 0 && len(i.InputFields) > 0 && i.CallbackMode == "" && i.CompletionInputKind == ""
	default:
		return false
	}
}

// ValidateGatewayConnectionInput checks only the bounded form envelope. It does
// not trim, normalize, decode a provider document, or emit submitted values.
// Provider parsing and exact target validation remain in the native adapter.
func ValidateGatewayConnectionInput(fields []GatewayConnectionField, input map[string]string) error {
	if !gatewayConnectionFieldsValid(fields) || len(input) > GatewayConnectionInputFields {
		return ErrGatewayNativeIdentity
	}
	allowed := make(map[string]GatewayConnectionField, len(fields))
	for _, f := range fields {
		allowed[f.FieldID] = f
		if f.Required && input[f.FieldID] == "" {
			return ErrGatewayNativeIdentity
		}
	}
	total := 0
	for name, value := range input {
		f, ok := allowed[name]
		if !ok || !utf8.ValidString(value) || len(value) > f.MaxBytes {
			return ErrGatewayNativeIdentity
		}
		total += len(value)
		if total > GatewayConnectionInputBytes {
			return ErrGatewayNativeIdentity
		}
		if f.Kind == "select" && value != "" {
			matched := false
			for _, choice := range f.Choices {
				matched = matched || value == choice.Value
			}
			if !matched {
				return ErrGatewayNativeIdentity
			}
		}
	}
	return nil
}

// CanonicalGatewayConnectionInput is transient keyed-commitment material. It is
// never a stored DTO. encoding/json sorts string keys, and leaves document/token
// values semantically untouched; the existing intent MAC owns secrecy/integrity.
func CanonicalGatewayConnectionInput(selection GatewayConnectionSelection, input map[string]string) ([]byte, error) {
	if !GatewayConnectionSelectionValid(selection) || len(input) > GatewayConnectionInputFields {
		return nil, ErrGatewayNativeIdentity
	}
	total := 0
	for name, value := range input {
		if !gatewayConnectionSemanticID.MatchString(name) || !utf8.ValidString(value) {
			return nil, ErrGatewayNativeIdentity
		}
		total += len(value)
	}
	if total > GatewayConnectionInputBytes {
		return nil, ErrGatewayNativeIdentity
	}
	return json.Marshal(struct {
		Contract   string                     `json:"contract"`
		Connection GatewayConnectionSelection `json:"connection"`
		Input      map[string]string          `json:"input_fields"`
	}{"native-connection-input/v1", selection, input})
}
