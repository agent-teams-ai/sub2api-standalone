package service

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"
)

const gatewayCodexSubscriptionURL = "https://chatgpt.com/backend-api/subscriptions"

// This optional auth/maintenance collector is separate from the cached reader.
// It uses the exact account selected by the authenticated accounts/check flow;
// a token claim, user field or another workspace cannot choose the query.
func gatewayCodexSubscriptionInfo(ctx context.Context, client *http.Client, headers http.Header, access, selected string, current GatewayAccountInfoFields) *GatewayAccountInfoFields {
	if client == nil || selected == "" || current.Subscription.Status != "known" || current.Subscription.Value == nil {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	target := gatewayCodexSubscriptionURL + "?" + url.Values{"account_id": {selected}}.Encode()
	request, err := http.NewRequestWithContext(bounded, http.MethodGet, target, nil)
	if err != nil {
		return nil
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Authorization", "Bearer "+access)
	request.Header.Set("Origin", "https://chatgpt.com")
	request.Header.Set("Referer", "https://chatgpt.com/")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil || response == nil || response.Body == nil {
		return nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 65537))
	closeErr := response.Body.Close()
	defer clear(raw)
	if readErr != nil || closeErr != nil || len(raw) > 65536 || bounded.Err() != nil || response.StatusCode != http.StatusOK || response.Request == nil || response.Request.URL.String() != target {
		return nil
	}
	fields, err := gatewayOAuthJSON(raw)
	if err != nil {
		return nil
	}
	// A recognized complete subscription response may remove a previous date.
	// An arbitrary JSON 200 and an incorrectly typed member are not an omission.
	if _, ok := fields["active_until"]; !ok {
		return nil
	}
	var expiry string
	if string(fields["active_until"]) != "null" {
		var valid bool
		expiry, valid = gatewayOAuthString(fields, "active_until")
		if !valid && string(fields["active_until"]) != `""` {
			return nil
		}
		if expiry != "" {
			expiry = normalizeChatGPTSubscriptionExpiry(expiry)
			if expiry == "" {
				return nil
			}
		}
	}
	if rawID, exists := fields["account_id"]; exists && string(rawID) != "null" {
		id, valid := gatewayOAuthString(fields, "account_id")
		if !valid || id != selected {
			return nil
		}
	}
	plan := *current.Subscription.Value
	plan.EndsAt = nil
	if expiry != "" {
		plan.EndsAt = &GatewayAccountInfoDate{Kind: "instant", Value: expiry}
	}
	out := GatewayAccountInfoEmptyFields(GatewayCodexOAuthResponsesProfile)
	out.Subscription = gatewayKnownFact(plan, "provider_api", time.Now().UTC())
	return &out
}
