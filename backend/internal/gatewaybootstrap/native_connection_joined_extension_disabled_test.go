//go:build linux && (integration || gatewaycustody) && !gatewayextension

package gatewaybootstrap

import (
	"net/http"
	"testing"
)

func newNativeConnectionJoinedExtensionFamily(t *testing.T, _ *Config) nativeConnectionJoinedFamily {
	t.Helper()
	t.Fatal("NOT_RUN: extensibility requires the explicit gatewayextension native build tag")
	return nativeConnectionJoinedFamily{}
}

func nativeConnectionJoinedExtensionTransport(_ string, transport http.RoundTripper) http.RoundTripper {
	return transport
}
