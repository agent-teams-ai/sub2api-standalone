package service

import (
	"context"
	"io"
	"net/http"
	"time"
)

// Quota uses the existing source collectors and snapshot table. This port has
// no enrollment, refresh, inference or account-info read capability.
type GatewayNativeConnectionQuotaRepository interface {
	ListGatewayNativeConnectionQuotaAccounts(context.Context) ([]GatewayNativeConnectionPhysical, error)
	StoreGatewayNativeConnectionQuota(context.Context, GatewayNativeConnectionPhysical, GatewayProviderQuotaObservation, int64) error
	ReserveGatewayAccountInfoOrder(context.Context) (int64, error)
}

func gatewayConnectionQuotaSource(profile string) string {
	if profile == GatewayCodexOAuthResponsesProfile {
		return "codex"
	}
	return GatewayAccountLimitsProviderV2(profile)
}

func (s *GatewayNativeConnections) ListQuotaAccounts(ctx context.Context) ([]GatewayNativeConnectionPhysical, error) {
	if s == nil || ctx == nil {
		return nil, ErrGatewayNativeIdentity
	}
	repository, ok := s.repository.(GatewayNativeConnectionQuotaRepository)
	if !ok {
		return nil, ErrGatewayNativeIdentity
	}
	candidates, err := repository.ListGatewayNativeConnectionQuotaAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]GatewayNativeConnectionPhysical, 0, len(candidates))
	for _, candidate := range candidates {
		// Creation configuration can be withdrawn while cached facts remain
		// readable. It does not authorize fresh provider reads under a new
		// client, target or selection.
		if _, configured := s.methods[candidate.Native.Publication.Connection]; configured && gatewayConnectionQuotaSource(candidate.Native.Profile) != "" {
			out = append(out, candidate)
		}
	}
	return out, nil
}

// CollectQuota is called only by source-owned maintenance. It holds the exact
// encrypted physical/revision lease through one bounded read-only fetch, then
// reacquires that identity for publication. It never refreshes a credential.
func (s *GatewayNativeConnections) CollectQuota(ctx context.Context, expected GatewayNativeConnectionPhysical, collector *GatewayProviderQuotaCollector) error {
	if s == nil || collector == nil {
		return ErrGatewayNativeIdentity
	}
	in := GatewayNativeConnectionScope{Scope: expected.Scope, Operation: expected.Operation, Connection: expected.Native.Publication.Connection}
	ctx, err := s.authorize(ctx, in)
	if err != nil || gatewayConnectionQuotaSource(expected.Native.Profile) == "" {
		return ErrGatewayNativeIdentity
	}
	if _, configured := s.methods[in.Connection]; !configured {
		return ErrGatewayNativeIdentity
	}
	repository, ok := s.repository.(GatewayNativeConnectionQuotaRepository)
	if !ok {
		return ErrGatewayNativeIdentity
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var order int64
	if GatewayAccountInfoPlanQuotaProfile(expected.Native.Profile) {
		order, err = repository.ReserveGatewayAccountInfoOrder(bounded)
		if err != nil {
			return ErrGatewayNativeIdentity
		}
	}
	physical, release, err := s.repository.LockGatewayNativeConnection(bounded, in.Scope, in.Operation, &expected.Native)
	if err != nil || release == nil {
		return ErrGatewayNativeIdentity
	}
	observation, err := func() (GatewayProviderQuotaObservation, error) {
		defer release()
		if physical.CredentialVersion != expected.CredentialVersion {
			return GatewayProviderQuotaObservation{}, ErrGatewayNativeIdentity
		}
		raw, err := AuthenticateGatewayNativeConnectionPhysical(bounded, s.custody, physical)
		defer clear(raw)
		if err != nil {
			return GatewayProviderQuotaObservation{}, ErrGatewayNativeIdentity
		}
		document, err := gatewayConnectionDocument(raw)
		if err != nil {
			return GatewayProviderQuotaObservation{}, ErrGatewayNativeIdentity
		}
		defer clear(document)
		account := *physical.Account
		account.Credentials = document
		provider := gatewayConnectionQuotaSource(physical.Native.Profile)
		if provider == "codex" {
			return s.fetchCodexImportQuota(bounded, &account)
		}
		credential := account.GetCredential("api_key")
		if provider == "grok" {
			bundle, err := gatewayGrokImportAccount(&account, in.Connection)
			if err != nil {
				return GatewayProviderQuotaObservation{}, ErrGatewayNativeIdentity
			}
			credential = bundle.AccessToken
		} else if physical.Native.Publication.Credential != (GatewayCredentialAttestation{CredentialKind: "api_key", CredentialPolicy: "provider_key", RefreshPolicy: "none"}) {
			return GatewayProviderQuotaObservation{}, ErrGatewayNativeIdentity
		}
		token := []byte(credential)
		defer clear(token)
		return collector.FetchWithAccountInfo(bounded, provider, token)
	}()
	if err != nil || bounded.Err() != nil {
		return ErrGatewayProviderQuota
	}
	return repository.StoreGatewayNativeConnectionQuota(bounded, physical, observation, order)
}

func (s *GatewayNativeConnections) fetchCodexImportQuota(ctx context.Context, account *Account) (GatewayProviderQuotaObservation, error) {
	empty := GatewayProviderQuotaObservation{}
	if s.codex == nil || s.codex.client == nil {
		return empty, ErrGatewayProviderQuota
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, chatGPTUsageURL, nil)
	if err != nil || gatewayCodexImportSetHeaders(account, request) != nil {
		return empty, ErrGatewayProviderQuota
	}
	request.Header.Set("Accept", "application/json")
	request.GetBody = nil
	response, err := s.codex.client.Do(request)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil || response == nil || response.Body == nil || response.StatusCode != http.StatusOK ||
		response.Request == nil || response.Request.URL.String() != chatGPTUsageURL {
		return empty, ErrGatewayProviderQuota
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(body) > 65536 || ctx.Err() != nil {
		return empty, ErrGatewayProviderQuota
	}
	observed := time.Now().UTC()
	raw, err := projectGatewayCodexQuota(body, account.GetCredential("chatgpt_account_id"), observed)
	if err != nil {
		return empty, ErrGatewayProviderQuota
	}
	snapshot := gatewayConnectionOrdinaryQuota(GatewayCodexOAuthResponsesProfile, NormalizeGatewayCodexLimits(raw))
	if !ValidateGatewayAccountLimitsSnapshotV3(snapshot, GatewayCodexOAuthResponsesProfile) {
		return empty, ErrGatewayProviderQuota
	}
	return GatewayProviderQuotaObservation{Limits: snapshot, ObservedAt: observed}, nil
}

func gatewayConnectionOrdinaryQuota(profile string, windows []GatewayAccountLimitWindow) GatewayAccountLimitsSnapshotV2 {
	projected := projectGatewayAccountLimitsV1V3(profile, GatewayAccountLimitsResult{Status: "snapshot", Scope: "account", Windows: windows})
	return GatewayAccountLimitsSnapshotV2{Scope: "account", Windows: projected.Windows}
}

// The common HTTP seam calls this after releasing its entry lock. Headers are
// already received from the exact admitted provider; publishing them cannot
// change inference settlement, replay it or fetch another provider response.
func captureGatewayNativeConnectionQuota(ctx context.Context, account *Account, response *http.Response) {
	if ctx == nil || response == nil || response.StatusCode < 200 || response.StatusCode >= 300 && response.StatusCode != http.StatusTooManyRequests {
		return
	}
	state, _ := ctx.Value(gatewayConnectionDispatchStateKey{}).(*gatewayConnectionDispatchState)
	if state == nil || !state.entered.Load() || account != state.account || state.binding.service == nil {
		return
	}
	repository, ok := state.binding.service.repository.(GatewayNativeConnectionQuotaRepository)
	if !ok {
		return
	}
	observed := time.Now().UTC()
	var snapshot GatewayAccountLimitsSnapshotV2
	switch state.binding.native.Profile {
	case GatewayCodexOAuthResponsesProfile:
		snapshot = gatewayConnectionOrdinaryQuota(state.binding.native.Profile, NormalizeGatewayCodexLimits(CaptureGatewayCodexLimits(response.Header, observed)))
	case GatewayClaudeOAuthMessagesProfile:
		snapshot = gatewayConnectionOrdinaryQuota(state.binding.native.Profile, NormalizeGatewayClaudeLimits(CaptureGatewayClaudeLimits(response.Header, observed)))
	case GatewayGrokOAuthResponsesProfile:
		snapshot = CaptureGatewayGrokRateLimits(response.Header, observed)
	default:
		return
	}
	if len(snapshot.Windows) == 0 || !ValidateGatewayAccountLimitsSnapshotV3(snapshot, state.binding.native.Profile) {
		return
	}
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = repository.StoreGatewayNativeConnectionQuota(bounded, state.physical, GatewayProviderQuotaObservation{Limits: snapshot, ObservedAt: observed}, 0)
}
