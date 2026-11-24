//go:build integration || gatewaycustody

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type connectionModelsPeer struct {
	target    *url.URL
	transport http.RoundTripper
}

func (p connectionModelsPeer) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	copyURL := *request.URL
	copyURL.Scheme, copyURL.Host = p.target.Scheme, p.target.Host
	cloned.URL = &copyURL
	cloned.Host = request.URL.Host
	response, err := p.transport.RoundTrip(cloned)
	if response != nil {
		response.Request = request
	}
	return response, err
}

// These are real migrated rows, sealed credentials, native journals and cache
// readers. The local OpenRouter peer serves metadata only; inference cannot be
// replaced by the rejecting dispatcher in this observation qualification.
func TestGatewayNativeConnectionPostgresObservations(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_OBSERVATIONS_TEST_DSN", "native_observations_test_")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	rows, ok := accounts.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	custody, err := service.NewGatewayNativeCredentialCustody("observation-fixture", map[string][]byte{"observation-fixture": bytes.Repeat([]byte{0x37}, 32)})
	require.NoError(t, err)
	var providerCalls atomic.Int32
	var quotaCalls atomic.Int32
	peer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotNil(t, r.TLS)
		if r.URL.Path == "/api/monitor/usage/quota/limit" {
			quotaCalls.Add(1)
			require.Equal(t, "api.z.ai", r.Host)
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "observation-synthetic-secret", r.Header.Get("Authorization"))
			require.Empty(t, r.Header.Get("Cookie"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"success":true,"data":{"level":"GLM Coding Max","limits":[{"type":"CREDIT_LIMIT","unit":3,"percentage":23.5}]}}`)
			return
		}
		providerCalls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Empty(t, r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("Cookie"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"vendor/anchor","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"supported_parameters":["tools"]},{"id":"vendor/second","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"supported_parameters":[]}]}`)
	}))
	defer peer.Close()
	target, err := url.Parse(peer.URL)
	require.NoError(t, err)
	providerTransport := connectionModelsPeer{target, peer.Client().Transport}
	gateway := &service.OpenAIGatewayService{}
	require.NoError(t, gateway.SetGatewayOpenRouterModels(service.NewGatewayOpenRouterModels(providerTransport)))
	definitions := []struct{ profile, base, model string }{
		{"deepseek-responses-apikey-v1", service.DefaultDeepseekBaseURL, "deepseek-v4-flash"},
		{service.GatewayGLMCodingResponsesProfile, service.GatewayGLMCodingResponsesBaseURL, service.GatewayGLMCodingResponsesModel},
		{service.GatewayOpenRouterResponsesProfile, service.GatewayOpenRouterBaseURL, "vendor/anchor"},
	}
	methods := make([]service.GatewayNativeConnectionMethodAdapter, 0, len(definitions))
	dispatchers := map[string]service.GatewayNativeConnectionDispatchAdapter{}
	for _, definition := range definitions {
		method, err := service.NewGatewayNativeAPIKeyConnectionMethod(definition.profile, "observations-config",
			service.GatewayConnectionMount{BaseURL: definition.base, Model: definition.model, QualificationRef: "observations-qualified"}, gateway)
		require.NoError(t, err)
		methods = append(methods, method)
		dispatchers[method.DispatchFamily] = service.GatewayNativeConnectionDispatchAdapter{
			Forward: func(context.Context, *gin.Context, *service.Account, service.GatewayNativeRoute, []byte) (*service.OpenAIForwardResult, error) {
				return nil, service.ErrGatewayNativeIdentity
			},
			ValidateRequest: func(*http.Request, *service.Account, service.GatewayNativePublishedRoute, string) bool { return false },
		}
	}
	owner := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
		if !service.GatewayConnectionScopeAuthorized(ctx, scope) {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	var cleanupRoute *service.GatewayNativeRoute
	dependencies := service.GatewayNativeConnectionsDependencies{Journal: journal, Repository: rows, Custody: custody,
		MaterialKey: bytes.Repeat([]byte{0x53}, 32), Methods: methods, Connect: owner, Refresh: owner,
		Cleanup: func(context.Context, service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
			if cleanupRoute == nil {
				return service.GatewayNativeOAuthCleanupAuthority{}, service.ErrGatewayNativeIdentity
			}
			return service.GatewayNativeOAuthCleanupAuthority{Owner: "observations-owner", LeaseExpiresAt: time.Now().Add(time.Minute), Native: cleanupRoute}, nil
		}, Dispatchers: dispatchers, Gateway: gateway}
	native, err := service.NewGatewayNativeConnections(dependencies)
	require.NoError(t, err)
	ownerCtx, err := service.WithGatewayNativeConsumer(ctx, "observations-consumer")
	require.NoError(t, err)
	ownerCtx, err = service.WithGatewayNativeOAuthOwner(ownerCtx, "observations-owner")
	require.NoError(t, err)
	inputs := make([]service.GatewayAccountLimitsInput, 0, len(methods))
	scopes := make([]service.GatewayNativeConnectionScope, 0, len(methods))
	routes := make(map[string]service.GatewayNativePublishedRoute)
	for _, method := range methods {
		in := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{Consumer: "observations-consumer", Owner: "observations-owner",
			Account: uuid.NewString(), Generation: uuid.NewString(), Purpose: service.GatewayOAuthBundlePurpose}, Operation: "observation-" + uuid.NewString(), Connection: method.Selection()}
		created, err := native.Begin(ownerCtx, in, map[string]string{"api_key": "observation-synthetic-secret"})
		require.NoError(t, err)
		require.Equal(t, "completed", created.State)
		descriptor, err := native.Describe(ownerCtx, in)
		require.NoError(t, err)
		require.NotNil(t, descriptor.Native)
		routes[in.Scope.Account] = *descriptor.Native
		inputs = append(inputs, service.GatewayAccountLimitsInput{OwnerRef: in.Scope.Owner, AccountRef: in.Scope.Account, ProfileID: method.ProfileID, ExpectedAuthorizationEpoch: 7})
		scopes = append(scopes, in)
	}
	var authorityCalls int
	var afterAuthority func()
	authorize := func(ctx context.Context, in service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
		authorityCalls++
		if afterAuthority != nil && authorityCalls%2 == 0 {
			afterAuthority()
		}
		route, found := routes[in.AccountRef]
		if ctx.Err() != nil || !found || in.OwnerRef != "observations-owner" || in.ExpectedAuthorizationEpoch != 7 || route.Profile != in.ProfileID {
			return service.GatewayNativeRoute{}, service.ErrGatewayNativeIdentity
		}
		return route.GatewayNativeRoute, nil
	}
	models, err := service.NewGatewayAccountModelsReaderV2(native, nil, authorize)
	require.NoError(t, err)
	limits, err := service.NewGatewayAccountLimitsReaderV3(native, nil, nil, authorize)
	require.NoError(t, err)
	info, err := service.NewGatewayAccountInfoReader(NewGatewayAccountInfoRepository(db), authorize, native)
	require.NoError(t, err)
	beforeInfo := providerCalls.Load()
	initialInfo := info.Read(ownerCtx, inputs[0])
	require.Equal(t, "available", initialInfo.Info.Status)
	require.Equal(t, "unsupported", initialInfo.Info.Fields.CredentialExpiresAt.Status)
	require.Equal(t, beforeInfo, providerCalls.Load(), "cached metadata cannot fetch provider models or quotas")
	deepseek := models.Read(ownerCtx, service.GatewayAccountModelsInput{GatewayAccountLimitsInput: inputs[0]})
	require.Equal(t, "snapshot", deepseek.Status)
	require.Equal(t, inputs[0].ProfileID, deepseek.SourceID)
	require.Equal(t, "deny", deepseek.Admission.UnavailableFallback)
	require.Len(t, *deepseek.Models, 1)
	require.Equal(t, definitions[0].model, (*deepseek.Models)[0].ModelID)
	wrongOwner := inputs[0]
	wrongOwner.OwnerRef = "foreign-owner"
	require.Equal(t, "unavailable", models.Read(ownerCtx, service.GatewayAccountModelsInput{GatewayAccountLimitsInput: wrongOwner}).Status)
	require.Equal(t, "unavailable", info.Read(ownerCtx, wrongOwner).Info.Status)
	require.Equal(t, "unsupported", limits.Read(ownerCtx, inputs[0]).Status)
	require.Equal(t, "unknown", limits.Read(ownerCtx, inputs[1]).Status)

	// The existing fixed provider collector observes actual TLS bytes and
	// atomically publishes quota plus plan facts under the encrypted revision.
	quotaRoute := routes[inputs[1].AccountRef]
	candidates, err := native.ListQuotaAccounts(ownerCtx)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	principal := service.GatewayNativeConnectionObservationPrincipal(candidates[0])
	collector := service.NewGatewayProviderQuotaCollector(providerTransport)
	foreignCtx, err := service.WithGatewayNativeOAuthOwner(ownerCtx, "foreign-owner")
	require.NoError(t, err)
	require.Error(t, native.CollectQuota(foreignCtx, candidates[0], collector))
	require.Zero(t, quotaCalls.Load())
	require.NoError(t, native.CollectQuota(ownerCtx, candidates[0], collector))
	require.EqualValues(t, 1, quotaCalls.Load())
	quota := limits.Read(ownerCtx, inputs[1])
	require.Equal(t, "snapshot", quota.Status)
	require.Equal(t, principal, quota.PrincipalVersion)
	require.Len(t, quota.Windows, 1)
	require.Equal(t, "five_hour", quota.Windows[0].ID)
	require.Equal(t, "credits", quota.Windows[0].Unit)
	require.Equal(t, 23.5, *quota.Windows[0].UsedPercent)
	plan := info.Read(ownerCtx, inputs[1])
	require.Equal(t, "available", plan.Info.Status)
	require.Equal(t, "known", plan.Info.Fields.Subscription.Status)
	require.Equal(t, "GLM Coding Max", plan.Info.Fields.Subscription.Value.Code)
	require.Equal(t, "provider_api", plan.Info.Fields.Subscription.Source)
	require.EqualValues(t, 1, quotaCalls.Load(), "cached quota/account-info reads never collect")

	openRouter := models.Read(ownerCtx, service.GatewayAccountModelsInput{GatewayAccountLimitsInput: inputs[2]})
	require.Equal(t, "snapshot", openRouter.Status)
	require.Len(t, *openRouter.Models, 2)
	require.Equal(t, []string{"none"}, (*openRouter.Models)[1].Tools.Allowed)
	beforeDispatch := providerCalls.Load()
	dispatchCtx, err := native.BindApproved(ownerCtx, routes[inputs[2].AccountRef].GatewayNativeRoute, inputs[2].AccountRef, 7, 65536, 1024)
	require.NoError(t, err)
	require.True(t, native.GatewayNativeConnectionModelAllowed(dispatchCtx, routes[inputs[2].AccountRef], "vendor/second"))
	require.False(t, native.GatewayNativeConnectionModelAllowed(dispatchCtx, routes[inputs[2].AccountRef], "vendor/unlisted"))
	require.Equal(t, beforeDispatch, providerCalls.Load(), "dispatch membership never fetches provider models")

	// Withdrawing all currently enabled methods cannot erase an existing
	// physical account's publication or turn metadata into a new selection.
	dependencies.Methods = nil
	recovery, err := service.NewGatewayNativeConnections(dependencies)
	require.NoError(t, err)
	recoveryModels, err := service.NewGatewayAccountModelsReaderV2(recovery, nil, authorize)
	require.NoError(t, err)
	require.Equal(t, "snapshot", recoveryModels.Read(ownerCtx, service.GatewayAccountModelsInput{GatewayAccountLimitsInput: inputs[0]}).Status)
	recoveryInfo, err := service.NewGatewayAccountInfoReader(NewGatewayAccountInfoRepository(db), authorize, recovery)
	require.NoError(t, err)
	require.Equal(t, initialInfo, recoveryInfo.Read(ownerCtx, inputs[0]), "unmounting does not erase or reinterpret original metadata")
	require.Error(t, recovery.CollectQuota(ownerCtx, candidates[0], collector))
	require.EqualValues(t, 1, quotaCalls.Load(), "removed configuration grants no new collection")

	// Native cleanup committed during the owner recheck must discard the old
	// snapshot. Reconnect binds a distinct birth/principal. No direct credential
	// UPDATE bypasses the database's custody/refresh guards for this race.
	route := routes[inputs[0].AccountRef]
	cleanupRoute = &route.GatewayNativeRoute
	authorityCalls = 0
	afterAuthority = func() {
		_, err := native.Cleanup(ownerCtx, scopes[0], service.GatewayNativeOAuthCleanupRequest{Scope: scopes[0].Scope, Operation: scopes[0].Operation, Action: "erase", CleanupRef: "observation-cleanup", CleanupToken: "observation-cleanup-token"})
		require.NoError(t, err)
	}
	require.Equal(t, "unavailable", models.Read(ownerCtx, service.GatewayAccountModelsInput{GatewayAccountLimitsInput: inputs[0]}).Status)
	afterAuthority = nil
	reconnected := scopes[0]
	reconnected.Scope.Generation, reconnected.Operation = uuid.NewString(), "observation-reconnect-"+uuid.NewString()
	created, err := native.Begin(ownerCtx, reconnected, map[string]string{"api_key": "rotated-observation-synthetic-secret"})
	require.NoError(t, err)
	require.Equal(t, "completed", created.State)
	replacement, err := native.Describe(ownerCtx, reconnected)
	require.NoError(t, err)
	require.NotNil(t, replacement.Native)
	routes[inputs[0].AccountRef] = *replacement.Native
	current := models.Read(ownerCtx, service.GatewayAccountModelsInput{GatewayAccountLimitsInput: inputs[0]})
	require.Equal(t, "snapshot", current.Status)
	require.NotEqual(t, deepseek.PrincipalVersion, current.PrincipalVersion)
	currentInfo := info.Read(ownerCtx, inputs[0])
	require.Equal(t, "available", currentInfo.Info.Status)
	require.NotEqual(t, initialInfo.PrincipalVersion, currentInfo.PrincipalVersion)
	quotaRoute = routes[inputs[1].AccountRef]
	cleanupRoute = &quotaRoute.GatewayNativeRoute
	authorityCalls = 0
	afterAuthority = func() {
		_, err := native.Cleanup(ownerCtx, scopes[1], service.GatewayNativeOAuthCleanupRequest{Scope: scopes[1].Scope, Operation: scopes[1].Operation, Action: "erase", CleanupRef: "info-cleanup", CleanupToken: "info-cleanup-token"})
		require.NoError(t, err)
	}
	require.Equal(t, "unavailable", info.Read(ownerCtx, inputs[1]).Info.Status, "erasure during final authority check discards cached fields")
	afterAuthority = nil
	require.Error(t, native.CollectQuota(ownerCtx, candidates[0], collector))
	require.EqualValues(t, 1, quotaCalls.Load(), "erasure prevents collection with a stale listing")
	public, err := json.Marshal([]any{current, quota, openRouter})
	require.NoError(t, err)
	require.False(t, strings.Contains(string(public), "synthetic-secret"))
	require.NotContains(t, string(public), "connection_bundle")
}
