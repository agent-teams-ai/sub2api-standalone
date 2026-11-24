package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGatewayAccountInfoV2PreservesStoredV1EvidenceAndFailsClosed(t *testing.T) {
	// Independent old-format fixture: migration must not fetch/re-observe facts.
	raw := []byte(`{"source":"verified_claims","order":17,"credentialRevision":4,"fields":{"email":{"status":"known","value":"owner@example.invalid","source":"verified_claim","observedAt":"2026-11-19T10:00:00Z","freshness":"stale"},"accountLabel":{"status":"unknown"},"subscription":{"status":"unknown"},"credentialExpiresAt":{"status":"unknown"},"privacyMode":{"status":"unsupported"}}}`)
	var observation GatewayAccountInfoObservation
	require.NoError(t, json.Unmarshal(raw, &observation))
	require.Equal(t, int64(17), observation.Order)
	require.Equal(t, int64(4), observation.CredentialRevision)
	require.Equal(t, "2026-11-19T10:00:00Z", observation.Fields.Email.ObservedAt)
	require.Equal(t, "stale", observation.Fields.Email.Freshness)
	require.Equal(t, "unsupported", observation.Fields.TrainingUsage.Status)
	require.True(t, GatewayAccountInfoObservationValid(GatewayCodexOAuthResponsesProfile, observation))
	for _, broken := range []string{
		`{"email":{"status":"unknown"}}`,
		`{"email":{"status":"unknown"},"accountLabel":{"status":"unknown"},"subscription":{"status":"unknown"},"credentialExpiresAt":{"status":"unknown"},"privacyMode":{"status":"unsupported"},"trainingUsage":{"status":"unsupported"}}`,
		`{"email":{"status":"unknown"},"accountLabel":{"status":"unknown"},"subscription":{"status":"unknown"},"credentialExpiresAt":{"status":"unknown"},"trainingUsage":{"status":"known","value":"private","source":"provider_api","observedAt":"2026-11-19T10:00:00Z","freshness":"current"}}`,
	} {
		var fields GatewayAccountInfoFields
		require.Error(t, json.Unmarshal([]byte(broken), &fields))
	}
}

func TestGatewayAccountInfoV2TrainingSourceAndLegacyProjection(t *testing.T) {
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	fields := GatewayAccountInfoEmptyFields(GatewayCodexOAuthResponsesProfile)
	fields.TrainingUsage = gatewayKnownFact("disabled", "provider_api", at)
	observation := GatewayAccountInfoObservation{Source: "training_settings", Order: 9, CredentialRevision: 2, Fields: fields}
	require.True(t, GatewayAccountInfoObservationValid(GatewayCodexOAuthResponsesProfile, observation))
	for _, wrong := range []string{"account_check", "credential", "verified_claims"} {
		invalid := observation
		invalid.Source = wrong
		require.False(t, GatewayAccountInfoObservationValid(GatewayCodexOAuthResponsesProfile, invalid))
	}
	projected, valid := GatewayAccountInfoProject(GatewayCodexOAuthResponsesProfile, GatewayAccountInfoStored{
		Revision: 5, CredentialRevision: 3,
		Sources: map[string]GatewayAccountInfoObservation{"training_settings": observation},
	}, at.Add(25*time.Hour))
	require.True(t, valid)
	require.Equal(t, "disabled", *projected.TrainingUsage.Value)
	require.Equal(t, "stale", projected.TrainingUsage.Freshness)
	result := GatewayAccountInfoResult{Info: GatewayAccountInfo{Status: "available", InfoRevision: "5", Fields: &projected}, PhysicalGeneration: "generation", PrincipalVersion: "principal"}
	legacy := ProjectGatewayAccountInfoV1(result)
	require.Equal(t, "unsupported", legacy.Info.Fields.PrivacyMode.Status)
	require.Equal(t, result.PhysicalGeneration, legacy.PhysicalGeneration)
	require.Equal(t, result.PrincipalVersion, legacy.PrincipalVersion)
	v1, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NotContains(t, string(v1), "trainingUsage")
	v2, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(v2), "privacyMode")
	require.Contains(t, string(v2), `"trainingUsage":{"status":"known","value":"disabled"`)
}
