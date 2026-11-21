package service

import (
	"context"
	"io"
	"net/http"
	"time"
)

func gatewayCodexImportModelsKey(p GatewayNativeConnectionPhysical, version string) (string, string) {
	prefix := "native-codex:" + gatewayConnectionModelPrincipal(p.Scope, p.Native.Publication.Connection) + ":"
	return prefix + GatewayNativeConnectionObservationPrincipal(p) + ":" + version, prefix
}

// The same native Codex cache/parser serves imported credentials. Owner, birth,
// method/configuration and credential revision partition every cached snapshot.
func (s *GatewayNativeConnections) codexImportModels(ctx context.Context, p GatewayNativeConnectionPhysical, force bool) GatewayAccountModelsResult {
	if s.codex == nil || s.codex.client == nil {
		return GatewayAccountModelsResult{Status: "unavailable"}
	}
	if s.codex.modelVersions != nil {
		s.codex.modelVersions.Refresh(ctx, force)
	}
	identity := resolveCodexOutboundIdentity("")
	key, prefix := gatewayCodexImportModelsKey(p, identity.version)
	result := s.codex.models.read(ctx, key, prefix, force, func() GatewayAccountModelsResult {
		return s.fetchCodexImportModels(ctx, p, identity)
	})
	if result.Status == "snapshot" && (result.PhysicalGeneration != p.Scope.Generation || result.PrincipalVersion != GatewayNativeConnectionObservationPrincipal(p)) {
		return GatewayAccountModelsResult{Status: "unavailable"}
	}
	return result
}

func (s *GatewayNativeConnections) fetchCodexImportModels(ctx context.Context, p GatewayNativeConnectionPhysical, identity codexOutboundIdentity) GatewayAccountModelsResult {
	unavailable := GatewayAccountModelsResult{Status: "unavailable"}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	fresh, release, err := s.repository.LockGatewayNativeConnection(bounded, p.Scope, p.Operation, &p.Native)
	if err != nil || release == nil {
		return unavailable
	}
	defer release()
	if fresh.CredentialVersion != p.CredentialVersion {
		return unavailable
	}
	raw, err := AuthenticateGatewayNativeConnectionPhysical(bounded, s.custody, fresh)
	defer clear(raw)
	if err != nil {
		return unavailable
	}
	document, err := gatewayCodexImportDecode(raw)
	if err != nil {
		return unavailable
	}
	defer clear(document)
	account := *fresh.Account
	account.Credentials = document
	endpoint := GatewayCodexOAuthBaseURL + "/models?client_version=" + identity.version
	request, err := http.NewRequestWithContext(bounded, http.MethodGet, endpoint, nil)
	if err != nil || gatewayCodexImportSetHeaders(&account, request) != nil {
		return unavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Originator", identity.originator)
	request.Header.Set("User-Agent", identity.userAgent)
	request.Header.Set("Version", identity.version)
	response, err := s.codex.client.Do(request)
	if err != nil || response == nil || response.Body == nil {
		return unavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK || response.Request == nil || response.Request.URL.String() != endpoint {
		return unavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return unavailable
	}
	models, err := projectGatewayCodexAccountModels(body, &gatewayCodexCatalogProjection{})
	if err != nil {
		return unavailable
	}
	now := time.Now().UTC()
	return GatewayAccountModelsResult{Status: "snapshot", Source: "codex", PhysicalGeneration: p.Scope.Generation,
		PrincipalVersion: GatewayNativeConnectionObservationPrincipal(p), FetchedAt: now.Format(time.RFC3339Nano),
		ExpiresAt: now.Add(gatewayAccountModelsSnapshotTTL).Format(time.RFC3339Nano), Models: &models}
}

func (s *GatewayNativeConnections) cachedCodexImportModels(p GatewayNativeConnectionPhysical) []GatewayAccountModel {
	if s.codex == nil {
		return nil
	}
	key, _ := gatewayCodexImportModelsKey(p, resolveCodexOutboundIdentity("").version)
	cache := &s.codex.models
	cache.Lock()
	entry, found := cache.entries[key]
	cache.Unlock()
	if !found || !time.Now().Before(entry.expires) || entry.result.Status != "snapshot" || entry.result.Models == nil ||
		entry.result.PhysicalGeneration != p.Scope.Generation || entry.result.PrincipalVersion != GatewayNativeConnectionObservationPrincipal(p) {
		return nil
	}
	return *entry.result.Models
}
