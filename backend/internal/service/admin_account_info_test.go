//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

type trainingInfoTransport func(*http.Request) (*http.Response, error)

func (f trainingInfoTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenAITrainingInfoRequiresFixedAuthenticatedSuccess(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		mode   string
	}{
		{"confirmed", http.StatusNoContent, "", PrivacyModeTrainingOff},
		{"forbidden", http.StatusForbidden, "denied", PrivacyModeFailed},
		{"cloudflare", http.StatusServiceUnavailable, "Just a moment", PrivacyModeCFBlocked},
		{"redirect", http.StatusFound, "", PrivacyModeFailed},
		{"oversize", http.StatusOK, strings.Repeat("x", 65537), PrivacyModeFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: trainingInfoTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, openAISettingsURL+"?feature=training_allowed&value=false", r.URL.String())
				require.Equal(t, http.MethodPatch, r.Method)
				require.Equal(t, "Bearer private-token", r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("Cookie"))
				require.Equal(t, "existing-firefox-client", r.Header.Get("User-Agent"))
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body)), Request: r,
					Header: http.Header{"Location": []string{"https://other.invalid/settings"}}}, nil
			})}
			jar, err := cookiejar.New(nil)
			require.NoError(t, err)
			target, err := url.Parse(openAISettingsURL)
			require.NoError(t, err)
			jar.SetCookies(target, []*http.Cookie{{Name: "unrelated-session", Value: "private-cookie"}})
			client.Jar = jar
			headers := http.Header{"User-Agent": []string{"existing-firefox-client"}, "Cookie": []string{"other-session=private"}}
			mode := disableOpenAITrainingWithClient(context.Background(), client, headers, "private-token")
			require.Equal(t, test.mode, mode)
			require.Equal(t, 1, calls, "a redirect must not turn into provider-confirmed success")
			fact := GatewayConfirmedTrainingInfo(GatewayCodexOAuthResponsesProfile, mode, time.Now().UTC())
			if mode == PrivacyModeTrainingOff {
				require.NotNil(t, fact)
				require.Equal(t, "disabled", *fact.TrainingUsage.Value)
			} else {
				require.Nil(t, fact, "operation errors cannot produce a training fact")
			}
		})
	}
}

func TestAdminAccountInfoCredentialReplacementAndSourceIntegrity(t *testing.T) {
	at := time.Now().UTC()
	fields := GatewayConfirmedTrainingInfo(GatewayCodexOAuthResponsesProfile, PrivacyModeTrainingOff, at)
	stored := GatewayAccountInfoStored{Revision: 1, CredentialRevision: 2, Sources: map[string]GatewayAccountInfoObservation{
		"training_settings": {Source: "training_settings", Order: 1, CredentialRevision: 1, Fields: *fields},
	}}
	protected, valid := GatewayAccountInfoProject(GatewayCodexOAuthResponsesProfile, stored, at)
	require.True(t, valid)
	require.Equal(t, "known", protected.TrainingUsage.Status, "verified protected principal may retain an account-level fact across rotation")
	ordinary := AdminAccountInfoProject(GatewayCodexOAuthResponsesProfile, stored, at)
	require.Equal(t, "unknown", ordinary.Fields.TrainingUsage.Status, "ordinary reauthorization has no verified protected principal")
	broken := stored.Sources["training_settings"]
	broken.Fields.TrainingUsage.Source = "imported"
	stored.Sources["training_settings"] = broken
	require.Equal(t, "unavailable", AdminAccountInfoProject(GatewayCodexOAuthResponsesProfile, stored, at).Status,
		"corrupt provenance is rejected even for an older credential revision")
}

func TestGatewayTrainingCollectorPreservesCustodyCommitBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := &http.Client{Transport: trainingInfoTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("optional HTTP must not consume the remaining mandatory publication budget")
		return nil, nil
	})}
	require.Nil(t, gatewayCodexTrainingInfo(ctx, client, nil, "private-token"))
}

func TestAdminAccountInfoAntigravityUsesOnlyObservedTier(t *testing.T) {
	at := time.Now().UTC()
	require.Nil(t, gatewayAntigravityAccountInfo(nil, at))
	missing := gatewayAntigravityAccountInfo(&antigravity.LoadCodeAssistResponse{}, at)
	require.Equal(t, "unknown", missing.Subscription.Status, "a missing tier is not the legacy Free default")
	response := &antigravity.LoadCodeAssistResponse{CurrentTier: &antigravity.TierInfo{ID: "free-tier"}, PaidTier: &antigravity.PaidTierInfo{ID: "g1-pro-tier"}}
	fields := gatewayAntigravityAccountInfo(response, at)
	require.Equal(t, "g1-pro-tier", fields.Subscription.Value.Code)
	require.Equal(t, "Pro", fields.Subscription.Value.Label)
	require.Equal(t, "provider_api", fields.Subscription.Source)
	require.Nil(t, fields.Subscription.Value.EndsAt)
	require.Equal(t, "unsupported", fields.TrainingUsage.Status)
	observation := GatewayAccountInfoObservation{Source: "plan_quota", Order: 1, CredentialRevision: 1, Fields: *fields}
	require.True(t, GatewayAccountInfoObservationValid("antigravity-responses-oauth-v1", observation))
	projected := AdminAccountInfoProject("antigravity-responses-oauth-v1", GatewayAccountInfoStored{Revision: 1, CredentialRevision: 1,
		Sources: map[string]GatewayAccountInfoObservation{"plan_quota": observation}}, at)
	require.Equal(t, fields.Subscription, projected.Fields.Subscription)
}
