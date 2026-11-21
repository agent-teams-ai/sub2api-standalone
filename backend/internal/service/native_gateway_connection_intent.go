package service

import (
	"net/url"
	"strings"
)

const GatewayNativeConnectionJournalClient = "native-connection-v1"
const GatewayNativeConnectionJournalMethod = "native"
const GatewayNativeConnectionAccountExtraKey = "gateway_native_connection_v1"
const GatewayNativeConnectionOperationExtraKey = "gateway_connection_operation_v1"

func GatewayConnectionMountValid(v GatewayConnectionMount) bool {
	u, err := url.Parse(v.BaseURL)
	return err == nil && len(v.BaseURL) <= 2048 && strings.TrimSpace(v.BaseURL) == v.BaseURL &&
		(u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && u.Fragment == "" && u.RawQuery == "" && u.Opaque == "" &&
		len(v.Model) <= 256 && GatewayNativeCredentialRefValid(v.Model) && len(v.QualificationRef) <= 128 && gatewayConnectionIdentifier.MatchString(v.QualificationRef)
}

func GatewayNativeConnectionAccountTypeValid(kind string) bool {
	switch kind {
	case AccountTypeAPIKey, AccountTypeOAuth, AccountTypeSetupToken, AccountTypeUpstream, AccountTypeBedrock, AccountTypeServiceAccount:
		return true
	default:
		return false
	}
}

func GatewayNativeConnectionIntent(in GatewayNativeOAuthConnectIntent) bool {
	return in.ClientID == GatewayNativeConnectionJournalClient
}

func gatewayConnectionIntentShapeValid(in GatewayNativeOAuthConnectIntent) bool {
	if !GatewayNativeConnectionIntent(in) {
		return in.NativeMount == nil && in.NativeDispatchFamily == "" && in.NativePlatform == "" && in.NativeAccountType == "" && in.NativePublication == nil
	}
	if in.Connection == nil || !GatewayConnectionSelectionValid(*in.Connection) || in.Method != GatewayNativeConnectionJournalMethod ||
		in.NativeMount == nil || !GatewayConnectionMountValid(*in.NativeMount) || !gatewayConnectionSemanticID.MatchString(in.NativeDispatchFamily) ||
		!gatewayConnectionSemanticID.MatchString(in.NativePlatform) || !GatewayNativeConnectionAccountTypeValid(in.NativeAccountType) ||
		in.Redirect != "" || !gatewayImportMACValid(in.ImportInputMAC) || !gatewayImportMACValid(in.ImportClaimMAC) || !gatewayImportMACValid(in.ImportKeyCommitment) ||
		in.DevicePhase != "" || in.DeviceRevision != 0 || !in.DevicePollAfter.IsZero() || !in.DeviceDeadline.IsZero() {
		return false
	}
	if in.NativePublication != nil && (!GatewayConnectionPublicationValid(*in.NativePublication) || in.NativePublication.Connection != *in.Connection || in.NativePublication.DispatchFamily != in.NativeDispatchFamily) {
		return false
	}
	return (in.State == "completed") == (in.NativePublication != nil)
}

func sameGatewayConnectionIntentConfig(a, b GatewayNativeOAuthConnectIntent) bool {
	if a.NativeDispatchFamily != b.NativeDispatchFamily || a.NativePlatform != b.NativePlatform || a.NativeAccountType != b.NativeAccountType || (a.NativeMount == nil) != (b.NativeMount == nil) {
		return false
	}
	return a.NativeMount == nil || *a.NativeMount == *b.NativeMount
}
