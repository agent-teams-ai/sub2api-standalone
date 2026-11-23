package service

// PlatformNativeConnectionInventory is the safe, generated declaration snapshot
// used by the existing catalog and qualification diff. It deliberately omits
// credentials, configured endpoints, generation and runtime availability: only
// qualified mounted bindings can make protected methods available.
type PlatformNativeConnectionInventory struct {
	Contract string                                     `json:"contract"`
	Profiles []PlatformNativeConnectionInventoryProfile `json:"profiles"`
}

type PlatformNativeConnectionInventoryProfile struct {
	ProfileID           string                                    `json:"profileId"`
	Platform            string                                    `json:"platform"`
	AccountMode         string                                    `json:"accountMode,omitempty"`
	Provider            GatewayAccountCapabilityLabel             `json:"provider"`
	Product             GatewayAccountCapabilityLabel             `json:"product"`
	Region              GatewayAccountCapabilityLabel             `json:"region"`
	Protocol            string                                    `json:"protocol"`
	DispatchFamily      string                                    `json:"dispatchFamily"`
	MetadataSources     []string                                  `json:"metadataSources"`
	LimitsContract      string                                    `json:"limitsContract"`
	UnavailableFallback string                                    `json:"unavailableFallback"`
	Methods             []PlatformNativeConnectionInventoryMethod `json:"methods"`
}

type PlatformNativeConnectionInventoryMethod struct {
	MethodID                  string                         `json:"methodId"`
	Revision                  int                            `json:"revision"`
	Label                     string                         `json:"label"`
	Create                    bool                           `json:"create"`
	Reconnect                 bool                           `json:"reconnect"`
	AllowedCredentialPolicies []string                       `json:"allowedCredentialPolicies"`
	Requirements              []string                       `json:"requirements"`
	ConfigurationRequirements []string                       `json:"configurationRequirements"`
	Interaction               GatewayConnectionInteraction   `json:"interaction"`
	AdminActions              []GatewayConnectionAdminAction `json:"adminActions"`
}

func buildPlatformNativeConnectionInventory(definitions []GatewayConnectionProfileDefinition) PlatformNativeConnectionInventory {
	result := PlatformNativeConnectionInventory{Contract: "native-connection-inventory/v1", Profiles: make([]PlatformNativeConnectionInventoryProfile, 0, len(definitions))}
	for _, definition := range definitions {
		profile := PlatformNativeConnectionInventoryProfile{
			ProfileID: definition.ProfileID, Platform: definition.Platform, AccountMode: definition.AccountMode,
			Provider: definition.Provider, Product: definition.Product, Region: definition.Region,
			Protocol: definition.Protocol, DispatchFamily: definition.DispatchFamily,
			MetadataSources: append([]string{}, definition.MetadataSources...), LimitsContract: definition.LimitsContract,
			UnavailableFallback: definition.UnavailableFallback, Methods: make([]PlatformNativeConnectionInventoryMethod, 0, len(definition.Methods)),
		}
		for _, method := range definition.Methods {
			// Select declaration fields only. A static export is not a live capability.
			descriptor := method.Descriptor("", false)
			profile.Methods = append(profile.Methods, PlatformNativeConnectionInventoryMethod{
				MethodID: descriptor.MethodID, Revision: descriptor.Revision, Label: descriptor.Label,
				Create: descriptor.Create, Reconnect: descriptor.Reconnect, AllowedCredentialPolicies: descriptor.AllowedCredentialPolicies,
				Requirements: descriptor.Requirements, ConfigurationRequirements: append([]string{}, method.ConfigurationRequirements...),
				Interaction: descriptor.Interaction, AdminActions: append([]GatewayConnectionAdminAction{}, method.AdminActions...),
			})
		}
		result.Profiles = append(result.Profiles, profile)
	}
	return result
}
