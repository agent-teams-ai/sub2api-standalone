//go:build linux

package gatewaybootstrap

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type privateConnectionBinding struct {
	selection service.GatewayConnectionSelection
	adapter   service.GatewayNativeConnectionAdapter
}

// This map is built once from real native lifecycle and dispatch bindings. It
// is neither a second provider catalog nor a runtime plugin registration API.
type privateConnections struct {
	capabilities service.GatewayAccountCapabilitiesV2
	bindings     map[string]map[string]privateConnectionBinding
	recovery     map[string]map[string]service.GatewayNativeConnectionAdapter
	owners       NativeOAuthOwnerAuthority
	cleanup      service.GatewayNativeOAuthCleanupAuthorizer
	custody      *service.GatewayNativeCredentialCustody
	authorize    func(*http.Request) (gatewaytransport.Peer, error)
}

func connectionConfigurationGeneration(c Config, profile gatewaytransport.QualifiedProfile) (string, error) {
	var key []byte
	for _, candidate := range c.Custody.Keys {
		if candidate.ID == c.Custody.ActiveKeyID {
			key, _ = base64.StdEncoding.Strict().DecodeString(candidate.Key)
		}
	}
	defer clear(key)
	if len(key) != 32 {
		return "", ErrDenied
	}
	// Commit deployment semantics with a server key; configuration containing
	// client credentials must never become a public unkeyed dictionary oracle.
	var authorization any
	switch profile.Profile {
	case service.GatewayCodexOAuthResponsesProfile:
		authorization = c.OAuth
	case service.GatewayGrokOAuthResponsesProfile:
		authorization = c.Grok
	}
	raw, err := json.Marshal(struct {
		Profile       gatewaytransport.QualifiedProfile
		Authorization any
	}{profile, authorization})
	if err != nil {
		return "", ErrDenied
	}
	defer clear(raw)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("account-gateway/native/connection-configuration/v1\x00"))
	_, _ = mac.Write(raw)
	return "cfg-" + hex.EncodeToString(mac.Sum(nil)), nil
}

func composePrivateConnectionBindings(c Config, profiles []gatewaytransport.QualifiedProfile, enrollment gatewaytransport.Enrollment, oauth *privateOAuth, custody *service.GatewayNativeCredentialCustody,
	auth func(*http.Request) (gatewaytransport.Peer, error), additional ...service.GatewayNativeConnectionAdapter) (*privateConnections, error) {
	if oauth == nil || oauth.owners == nil || custody == nil || auth == nil || len(profiles) > 128 {
		return nil, ErrDenied
	}
	out := &privateConnections{
		capabilities: service.GatewayAccountCapabilitiesV2{Contract: service.GatewayAccountCapabilitiesContractV2, Profiles: []service.GatewayConnectionCapability{}},
		bindings:     make(map[string]map[string]privateConnectionBinding), recovery: make(map[string]map[string]service.GatewayNativeConnectionAdapter),
		owners: oauth.owners, cleanup: oauth.cleanup, custody: custody, authorize: auth,
	}
	out.capabilities.Enrollment = service.GatewayConnectionEnrollment{OriginRef: enrollment.OriginRef,
		EngineIncarnation: enrollment.EngineIncarnation, QualificationRef: enrollment.QualificationRef}
	adapters := append([]service.GatewayNativeConnectionAdapter(nil), additional...)
	add := func(adapter service.GatewayNativeConnectionAdapter, err error) error {
		if err != nil {
			return ErrDenied
		}
		adapters = append(adapters, adapter)
		return nil
	}
	if oauth.connect != nil && oauth.dispatch != nil && oauth.refresh != nil {
		if add(service.NewGatewayNativeCodexConnectionAdapter(oauth.connect, oauth.dispatch, oauth.refresh, oauth.cleanup)) != nil {
			return nil, ErrDenied
		}
	}
	if oauth.claude != nil {
		if add(service.NewGatewayNativeClaudeConnectionAdapter(oauth.claude, oauth.cleanup)) != nil {
			return nil, ErrDenied
		}
	}
	if oauth.grok != nil {
		if add(service.NewGatewayNativeGrokConnectionAdapter(oauth.grok, oauth.cleanup)) != nil {
			return nil, ErrDenied
		}
	}
	for _, adapter := range adapters {
		if adapter.ProfileID == "" || adapter.Purpose == "" || len(adapter.MethodIDs) > 32 || adapter.Begin == nil ||
			adapter.Read == nil || adapter.Submit == nil || adapter.Describe == nil || adapter.Refresh == nil || adapter.Custody == nil || adapter.Cleanup == nil {
			return nil, ErrDenied
		}
		if out.recovery[adapter.ProfileID] == nil {
			out.recovery[adapter.ProfileID] = make(map[string]service.GatewayNativeConnectionAdapter)
		}
		for _, method := range adapter.MethodIDs {
			if _, duplicate := out.recovery[adapter.ProfileID][method]; duplicate {
				return nil, ErrDenied
			}
			out.recovery[adapter.ProfileID][method] = adapter
		}
	}
	for _, mounted := range profiles {
		definition, found := service.LookupGatewayConnectionProfile(mounted.Profile)
		generation, err := connectionConfigurationGeneration(c, mounted)
		if !found || err != nil || out.bindings[mounted.Profile] != nil {
			return nil, ErrDenied
		}
		methods := make(map[string]privateConnectionBinding)
		out.bindings[mounted.Profile] = methods
		capability := service.GatewayConnectionCapability{
			ProfileID: mounted.Profile, Provider: definition.Provider, Product: definition.Product, Region: definition.Region,
			Protocol: definition.Protocol, Models: service.GatewayNativeConnectionModels(mounted.Profile, mounted.Model),
			ModelPolicy: service.GatewayConnectionModelPolicy{SourceID: mounted.Profile, Contract: "native-account-models/v2", UnavailableFallback: definition.UnavailableFallback},
			Methods:     []service.GatewayConnectionMethod{}, AccountInfo: oauth.info != nil, LimitsContract: definition.LimitsContract,
			Availability: "deployment_configuration_required", Mount: service.GatewayConnectionMount{BaseURL: mounted.BaseURL, Model: mounted.Model, QualificationRef: mounted.QualificationRef},
			LegacyAliases: []service.GatewayConnectionLegacyAlias{},
		}
		for _, method := range definition.Methods {
			selection := service.GatewayConnectionSelection{ProfileID: mounted.Profile, MethodID: method.MethodID, MethodRevision: method.Revision, ConfigurationGeneration: generation}
			for _, adapter := range adapters {
				if adapter.ProfileID != mounted.Profile || adapter.DispatchFamily != definition.DispatchFamily || !adapter.Supports(method) {
					continue
				}
				if _, duplicate := methods[method.MethodID]; duplicate {
					return nil, ErrDenied
				}
				methods[method.MethodID] = privateConnectionBinding{selection: selection, adapter: adapter}
			}
			_, enabled := methods[method.MethodID]
			descriptor := method.Descriptor(generation, enabled)
			if !service.GatewayConnectionMethodValid(descriptor) {
				return nil, ErrDenied
			}
			capability.Methods = append(capability.Methods, descriptor)
			if enabled {
				capability.Availability = "enabled"
				if alias, supported := method.LegacyAlias(selection); supported {
					capability.LegacyAliases = append(capability.LegacyAliases, alias)
				}
			}
		}
		out.capabilities.Profiles = append(out.capabilities.Profiles, capability)
	}
	return out, nil
}
