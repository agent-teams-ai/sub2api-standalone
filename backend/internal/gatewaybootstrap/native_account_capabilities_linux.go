//go:build linux

package gatewaybootstrap

import (
	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func privateAccountCapabilities(profile ProfileConfig, transport *gatewaytransport.Handler, oauth *privateOAuth) service.GatewayAccountCapabilities {
	return privateAccountCapabilitiesForProfiles(transport.QualifiedProfiles(), oauth)
}

func privateAccountCapabilitiesForProfiles(profiles []gatewaytransport.QualifiedProfile, oauth *privateOAuth) service.GatewayAccountCapabilities {
	out := service.GatewayAccountCapabilities{Contract: "native-account-capabilities/v1", Profiles: []service.GatewayAccountCapability{}}
	info := oauth != nil && oauth.info != nil
	imports := oauth != nil && oauth.connect != nil && oauth.dispatch != nil
	for _, p := range profiles {
		// A qualified mount does not by itself provide the control-plane
		// lifecycle. V1 callers must only see methods with concrete routes.
		switch p.Profile {
		case service.GatewayCodexOAuthResponsesProfile:
			if !imports {
				continue
			}
		case service.GatewayClaudeOAuthMessagesProfile:
			if oauth == nil || oauth.claude == nil {
				continue
			}
		case service.GatewayGrokOAuthResponsesProfile:
			if oauth == nil || oauth.grok == nil {
				continue
			}
		}
		// V1 only names mounts understood by its existing concrete routes. New
		// native forms are projected through explicit V2 legacy aliases, never
		// advertised to an old consumer with an unusable credential endpoint.
		if !service.GatewayNativeAPIKeyProfileValid(p.Profile, p.BaseURL, p.Model) &&
			p.Profile != service.GatewayCodexOAuthResponsesProfile && p.Profile != service.GatewayClaudeOAuthMessagesProfile && p.Profile != service.GatewayGrokOAuthResponsesProfile {
			continue
		}
		capability, ok := service.GatewayNativeAccountCapability(p.Profile, p.Model, info, imports)
		if !ok {
			continue
		}
		if oauth != nil && oauth.connections != nil {
			aliases := map[string]bool{}
			for _, current := range oauth.connections.capabilities.Profiles {
				if current.ProfileID == p.Profile {
					for _, alias := range current.LegacyAliases {
						aliases[alias.Kind] = true
					}
				}
			}
			methods := make([]service.GatewayAccountConnectionMethod, 0, len(capability.Methods))
			for _, method := range capability.Methods {
				kind := method.Kind
				if kind == "credential_import" {
					kind += ":" + method.Method
				}
				if aliases[kind] {
					methods = append(methods, method)
				}
			}
			capability.Methods = methods
		}
		if len(capability.Methods) > 0 {
			out.Profiles = append(out.Profiles, capability)
		}
	}
	return out
}
