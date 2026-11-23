package service

import (
	"context"
	"io"
	"net/http"
	"time"
)

// Both existing admin privacy actions and protected enrollment/refresh use this
// exact authenticated, fixed-endpoint producer. Operation failure is not a fact.
func disableOpenAITrainingWithClient(ctx context.Context, client *http.Client, headers http.Header, accessToken string) string {
	if client == nil || accessToken == "" {
		return ""
	}
	const target = openAISettingsURL + "?feature=training_allowed&value=false"
	request, err := http.NewRequestWithContext(ctx, http.MethodPatch, target, nil)
	if err != nil {
		return PrivacyModeFailed
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Del("Cookie")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Origin", "https://chatgpt.com")
	request.Header.Set("Referer", "https://chatgpt.com/")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("sec-fetch-mode", "cors")
	request.Header.Set("sec-fetch-site", "same-origin")
	request.Header.Set("sec-fetch-dest", "empty")
	boundedClient := *client
	boundedClient.Jar = nil // The trusted result belongs to this bearer, not shared browser cookies.
	boundedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := boundedClient.Do(request)
	if err != nil || response == nil || response.Body == nil {
		return PrivacyModeFailed
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 65537))
	closeErr := response.Body.Close()
	defer clear(raw)
	if readErr != nil || closeErr != nil || len(raw) > 65536 || ctx.Err() != nil || response.Request == nil || response.Request.URL.String() != target {
		return PrivacyModeFailed
	}
	if (response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusServiceUnavailable) &&
		isCloudflareChallengeResponse(response.Header.Get("cf-mitigated"), string(raw)) {
		return PrivacyModeCFBlocked
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return PrivacyModeFailed
	}
	return PrivacyModeTrainingOff
}

func gatewayCodexTrainingInfo(ctx context.Context, client *http.Client, headers http.Header, access string) *GatewayAccountInfoFields {
	if deadline, bounded := ctx.Deadline(); bounded && time.Until(deadline) <= 4*time.Second {
		return nil // Preserve time for mandatory custody publication.
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	mode := disableOpenAITrainingWithClient(bounded, client, headers, access)
	return GatewayConfirmedTrainingInfo(GatewayCodexOAuthResponsesProfile, mode, time.Now().UTC())
}

func hasCurrentAdminTrainingInfo(ctx context.Context, repo AccountRepository, account *Account) bool {
	reader, ok := repo.(AdminAccountInfoRepository)
	if !ok {
		// Compatibility for repositories without the optional fact port; this
		// legacy operational marker is never upgraded into a normalized fact.
		return shouldSkipOpenAIPrivacyEnsure(account.Extra)
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	infos, err := reader.ReadAdminAccountInfo(bounded, []*Account{account})
	if err != nil || infos[account.ID].Fields == nil {
		return false
	}
	fact := infos[account.ID].Fields.TrainingUsage
	return fact.Status == "known" && fact.Source == "provider_api" && fact.Freshness == "current" && fact.Value != nil && *fact.Value == "disabled"
}

func persistAdminOpenAIPrivacy(ctx context.Context, repo AccountRepository, account *Account, factory PrivacyClientFactory, proxyURL string) string {
	if account == nil || HasGatewayNativeIdentity(account) || account.IsCredentialShadow() || factory == nil {
		return ""
	}
	writer, trusted := repo.(AdminAccountInfoRepository)
	if !trusted {
		mode := disableOpenAITraining(ctx, factory, account.GetCredential("access_token"), proxyURL)
		if mode != "" {
			_ = repo.UpdateExtra(ctx, account.ID, map[string]any{"privacy_mode": mode})
		}
		return mode
	}
	expected, err := cloneAccountJSONMap(account.Credentials)
	if err != nil {
		return ""
	}
	defer clear(expected)
	binding, err := writer.ReserveAdminAccountInfo(ctx, account)
	if err != nil {
		return ""
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client, err := factory(proxyURL)
	mode := PrivacyModeFailed
	var observations []GatewayAccountInfoObservation
	if err == nil && client != nil {
		httpClient := *client.GetClient()
		httpClient.Jar = nil
		httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		headers := client.Headers.Clone()
		headers.Del("Cookie")
		headers.Del("ChatGPT-Account-Id")
		access := account.GetCredential("access_token")
		checkContext, stopCheck := context.WithTimeout(bounded, 2*time.Second)
		check := &GatewayNativeOAuthDispatch{client: &httpClient, accountCheckHeaders: headers}
		selected, fields, checkErr := check.accountCheckInfo(checkContext, access)
		stopCheck()
		if checkErr == nil && (account.GetCredential("chatgpt_account_id") == "" || account.GetCredential("chatgpt_account_id") == selected) {
			observations = append(observations, GatewayAccountInfoObservation{Source: "account_check", Fields: fields})
			if fields.Subscription.Status == "known" && fields.Subscription.Value != nil && fields.Subscription.Value.EndsAt == nil {
				if subscription := gatewayCodexSubscriptionInfo(bounded, &httpClient, headers, access, selected, fields); subscription != nil {
					observations = append(observations, GatewayAccountInfoObservation{Source: "subscription_status", Fields: *subscription})
				}
			}
		}
		mode = disableOpenAITrainingWithClient(bounded, &httpClient, headers, access)
		if training := GatewayConfirmedTrainingInfo(binding.Profile, mode, time.Now().UTC()); training != nil {
			observations = append(observations, GatewayAccountInfoObservation{Source: "training_settings", Fields: *training})
		}
	}
	for i := range observations {
		observations[i].Order = binding.Order
		observations[i].CredentialRevision = binding.CredentialRevision
	}
	if err := writer.StoreAdminAccountInfo(ctx, binding, expected, mode, observations...); err != nil {
		return mode
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra["privacy_mode"] = mode
	populateAdminAccountInfo(ctx, repo, []*Account{account})
	return mode
}
