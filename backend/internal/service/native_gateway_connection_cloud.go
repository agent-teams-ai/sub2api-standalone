package service

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

var gatewayCloudAWSRegion = regexp.MustCompile(`^[a-z]{2}(?:-[a-z0-9]+){1,3}-[0-9]{1,2}$`)
var gatewayCloudProject = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// Cloud mounts identify qualified official services. The immutable input binds
// region/project/location; neither input nor a service-account token_uri may
// supply an arbitrary endpoint, proxy or dispatch implementation.
func gatewayCloudMountValid(family string, mount GatewayConnectionMount) bool {
	if !GatewayConnectionMountValid(mount) || !GatewayAccountModelIDValid(mount.Model) {
		return false
	}
	u, err := url.Parse(mount.BaseURL)
	if err != nil || u.Scheme != "https" || u.Port() != "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	switch family {
	case "bedrock_messages":
		const prefix, suffix = "bedrock-runtime.", ".amazonaws.com"
		return strings.HasPrefix(u.Host, prefix) && strings.HasSuffix(u.Host, suffix) &&
			gatewayCloudAWSRegion.MatchString(strings.TrimSuffix(strings.TrimPrefix(u.Host, prefix), suffix))
	case "vertex_messages", "vertex_gemini":
		if u.Host == vertexGlobalHost {
			return true
		}
		const suffix = "-aiplatform.googleapis.com"
		location := strings.TrimSuffix(u.Host, suffix)
		return strings.HasSuffix(u.Host, suffix) && len(location) <= 64 && vertexLocationPattern.MatchString(location)
	}
	return false
}

func gatewayCloudSecretValid(value string, limit int, required bool) bool {
	if len(value) > limit || required && value == "" {
		return false
	}
	for _, c := range value {
		if c <= ' ' || c >= 127 {
			return false
		}
	}
	return true
}

func gatewayCloudVertexContext(input map[string]string, defaultProject string) (string, string, error) {
	project := strings.TrimSpace(input["project_id"])
	if project == "" {
		project = defaultProject
	}
	location := strings.TrimSpace(input["location"])
	if location == "" {
		location = vertexDefaultLocation
	}
	if project != "" && !gatewayCloudProject.MatchString(project) || len(location) > 64 || !vertexLocationPattern.MatchString(location) {
		return "", "", ErrGatewayNativeIdentity
	}
	return project, location, nil
}

func gatewayCloudDocument(method GatewayConnectionMethodDefinition, input map[string]string) (json.RawMessage, error) {
	if ValidateGatewayConnectionInput(method.Interaction.InputFields, input) != nil {
		return nil, ErrGatewayNativeIdentity
	}
	credentials := make(map[string]any)
	switch method.MethodID {
	case "aws_sigv4", "bedrock_api_key":
		region := strings.TrimSpace(input["region"])
		if !gatewayCloudAWSRegion.MatchString(region) {
			return nil, ErrGatewayNativeIdentity
		}
		credentials["aws_region"] = region
		if method.MethodID == "bedrock_api_key" {
			if !GatewayNativeIngressKeyValid(input["api_key"]) {
				return nil, ErrGatewayNativeIdentity
			}
			credentials["auth_mode"], credentials["api_key"] = "apikey", input["api_key"]
		} else {
			if !gatewayCloudSecretValid(input["access_key_id"], 256, true) ||
				!gatewayCloudSecretValid(input["secret_access_key"], 4096, true) ||
				!gatewayCloudSecretValid(input["session_token"], 16384, false) {
				return nil, ErrGatewayNativeIdentity
			}
			credentials["aws_access_key_id"], credentials["aws_secret_access_key"] = input["access_key_id"], input["secret_access_key"]
			if value := input["session_token"]; value != "" {
				credentials["aws_session_token"] = value
			}
		}
	case "vertex_service_account":
		raw := []byte(input["service_account_json"])
		if _, err := gatewayOAuthJSON(raw); err != nil {
			return nil, ErrGatewayNativeIdentity
		}
		key, err := parseVertexServiceAccountJSON(raw)
		if err != nil || key.Type != "service_account" || !gatewayCloudProject.MatchString(key.ProjectID) ||
			!gatewayConnectionTextValid(key.ClientEmail, 320, false) || strings.TrimSpace(key.ClientEmail) != key.ClientEmail ||
			!gatewayConnectionTextValid(key.PrivateKeyID, 256, true) {
			return nil, ErrGatewayNativeIdentity
		}
		if _, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(key.PrivateKey)); err != nil {
			return nil, ErrGatewayNativeIdentity
		}
		project, location, err := gatewayCloudVertexContext(input, key.ProjectID)
		if err != nil || project == "" {
			return nil, ErrGatewayNativeIdentity
		}
		// Reuse the native parser's fixed Google token endpoint, and retain only
		// fields that its signing implementation consumes.
		normalized, err := json.Marshal(key)
		if err != nil {
			return nil, ErrGatewayNativeIdentity
		}
		credentials["service_account_json"], credentials["project_id"], credentials["location"] = string(normalized), project, location
	case "vertex_api_key":
		if !GatewayNativeIngressKeyValid(input["api_key"]) {
			return nil, ErrGatewayNativeIdentity
		}
		project, location, err := gatewayCloudVertexContext(input, "")
		if err != nil {
			return nil, err
		}
		credentials["auth_mode"], credentials["api_key"] = vertexAuthModeAPIKey, input["api_key"]
		credentials["project_id"], credentials["location"] = project, location
	default:
		return nil, ErrGatewayNativeIdentity
	}
	raw, err := json.Marshal(credentials)
	if err != nil || len(raw) > GatewayConnectionInputBytes {
		return nil, ErrGatewayNativeIdentity
	}
	return raw, nil
}

// NewGatewayNativeCloudConnectionMethod adapts the existing Bedrock/Vertex
// credential grammar. It does no network probing or credential rotation at
// setup. Vertex's short-lived access tokens are renewed by native dispatch;
// the stored service-account private key itself is not a rotating token.
func NewGatewayNativeCloudConnectionMethod(profileID, methodID, generation string, mount GatewayConnectionMount) (GatewayNativeConnectionMethodAdapter, error) {
	var empty GatewayNativeConnectionMethodAdapter
	definition, found := LookupGatewayConnectionProfile(profileID)
	if !found || !gatewayCloudMountValid(definition.DispatchFamily, mount) {
		return empty, ErrGatewayNativeIdentity
	}
	var method GatewayConnectionMethodDefinition
	for _, candidate := range definition.Methods {
		if candidate.MethodID == methodID && candidate.Interaction.Kind == "credential_form" &&
			len(candidate.AllowedCredentialPolicies) == 1 && candidate.AllowedCredentialPolicies[0] == "cloud_identity" {
			method = candidate
		}
	}
	selection := GatewayConnectionSelection{ProfileID: profileID, MethodID: method.MethodID, MethodRevision: method.Revision, ConfigurationGeneration: generation}
	if !GatewayConnectionSelectionValid(selection) {
		return empty, ErrGatewayNativeIdentity
	}
	attestation := GatewayCredentialAttestation{CredentialKind: "native", CredentialPolicy: "cloud_identity", RefreshPolicy: "none"}
	authFamily, accountType := "google_service_account", AccountTypeServiceAccount
	switch method.MethodID {
	case "aws_sigv4":
		authFamily, accountType = "aws_sigv4", AccountTypeBedrock
	case "bedrock_api_key":
		authFamily, accountType, attestation.CredentialKind = "bedrock_api_key", AccountTypeBedrock, "api_key"
	case "vertex_api_key":
		authFamily, attestation.CredentialKind = "vertex_api_key", "api_key"
	case "vertex_service_account":
	default:
		return empty, ErrGatewayNativeIdentity
	}
	return GatewayNativeConnectionMethodAdapter{
		ProfileID: profileID, MethodID: method.MethodID, MethodRevision: method.Revision,
		ConfigurationGeneration: generation, DispatchFamily: definition.DispatchFamily, AccountType: accountType, Mount: mount,
		Prepare: func(input map[string]string) (GatewayNativeConnectionPrepared, error) {
			document, err := gatewayCloudDocument(method, input)
			if err != nil {
				return GatewayNativeConnectionPrepared{}, err
			}
			material, err := json.Marshal(input)
			if err != nil || len(material) > GatewayConnectionInputBytes {
				clear(document)
				return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
			}
			return GatewayNativeConnectionPrepared{Material: material, EntryClaim: document, Credential: attestation, AuthFamily: authFamily}, nil
		},
		Complete: func(ctx context.Context, in GatewayNativeConnectionScope, material json.RawMessage, completion map[string]string) (GatewayNativeConnectionCredential, error) {
			if ctx == nil || ctx.Err() != nil || in.Connection != selection || len(completion) != 0 ||
				!GatewayConnectionScopeAuthorized(ctx, in.Scope) || !GatewayConnectionSelectionAuthorized(ctx, &in.Connection) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			if _, err := gatewayOAuthJSON(material); err != nil {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			var input map[string]string
			if json.Unmarshal(material, &input) != nil {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			defer clear(input)
			document, err := gatewayCloudDocument(method, input)
			if err != nil {
				return GatewayNativeConnectionCredential{}, err
			}
			return GatewayNativeConnectionCredential{Document: document, Credential: attestation, AuthFamily: authFamily}, nil
		},
	}, nil
}
