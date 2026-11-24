package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

// Existing create/refresh maintenance calls this only after durable credentials.
// Browser-returned PlanType and legacy stored plan_type are never fact receipts.
func observeAdminAntigravityInfo(ctx context.Context, repo AccountRepository, proxies ProxyRepository, account *Account) {
	if account == nil || HasGatewayNativeIdentity(account) || account.IsCredentialShadow() ||
		account.Platform != PlatformAntigravity || account.Type != AccountTypeOAuth || account.GetCredential("access_token") == "" {
		return
	}
	writer, ok := repo.(AdminAccountInfoRepository)
	if !ok {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if current, err := writer.ReadAdminAccountInfo(bounded, []*Account{account}); err == nil {
		if info := current[account.ID]; info.Fields != nil && info.Fields.Subscription.Status == "known" &&
			info.Fields.Subscription.Source == "provider_api" && info.Fields.Subscription.Freshness == "current" {
			return
		}
	}
	credentials, err := cloneAccountJSONMap(account.Credentials)
	if err != nil {
		return
	}
	defer clear(credentials)
	copy := *account
	copy.Credentials = credentials
	binding, err := writer.ReserveAdminAccountInfo(bounded, &copy)
	if err != nil {
		return
	}
	var proxyURL string
	if account.ProxyID != nil {
		if proxies == nil {
			return
		}
		proxy, err := proxies.GetByID(bounded, *account.ProxyID)
		if err != nil || proxy == nil {
			return
		}
		proxyURL = proxy.URL()
	}
	client, err := antigravity.NewClient(proxyURL)
	if err != nil {
		return
	}
	response, _, err := client.LoadCodeAssist(bounded, copy.GetCredential("access_token"))
	if err != nil {
		return
	}
	fields := gatewayAntigravityAccountInfo(response, time.Now().UTC())
	if fields == nil {
		return
	}
	observation := GatewayAccountInfoObservation{Source: "plan_quota", Order: binding.Order,
		CredentialRevision: binding.CredentialRevision, Fields: *fields}
	_ = writer.StoreAdminAccountInfo(bounded, binding, credentials, "", observation)
}
