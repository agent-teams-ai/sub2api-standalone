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
	add := func(id, model string) {
		if p, ok := service.GatewayNativeAccountCapability(id, model, info, imports); ok {
			out.Profiles = append(out.Profiles, p)
		}
	}
	for _, p := range profiles {
		add(p.Profile, p.Model)
	}
	return out
}
