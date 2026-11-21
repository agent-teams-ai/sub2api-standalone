//go:build !gatewayextension

package service

// The production inventory has no extension provider. A separate qualification
// build can append a source-owned fixture to this same canonical inventory.
func gatewayNativeConnectionExtensionInventory() []GatewayConnectionProfileDefinition { return nil }
