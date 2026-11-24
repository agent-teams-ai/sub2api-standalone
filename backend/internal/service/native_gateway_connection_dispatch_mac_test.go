package service

import (
	"encoding/json"
	"testing"
)

func TestGatewayConnectionDocumentKeyedMACBindsExactCredentials(t *testing.T) {
	raw := json.RawMessage(`{"api_key":"fixture-secret","other":"value"}`)
	var keyA, keyB [32]byte
	keyA[0] = 1
	keyB[0] = 2

	document, first, err := gatewayConnectionDocumentMAC(raw, &keyA)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(document)
	_, again, err := gatewayConnectionDocumentMAC(raw, &keyA)
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatal("identical credentials with the same request key changed MAC")
	}
	_, differentKey, err := gatewayConnectionDocumentMAC(raw, &keyB)
	if err != nil {
		t.Fatal(err)
	}
	if first == differentKey {
		t.Fatal("MAC must be bound to a freshly selected request key")
	}
	_, changedCredentials, err := gatewayConnectionDocumentMAC(json.RawMessage(`{"api_key":"tampered","other":"value"}`), &keyA)
	if err != nil {
		t.Fatal(err)
	}
	if first == changedCredentials {
		t.Fatal("changed credentials must not match the request MAC")
	}
	if _, _, err := gatewayConnectionDocumentMAC(raw, nil); err == nil {
		t.Fatal("missing authentication key must fail closed")
	}
}
