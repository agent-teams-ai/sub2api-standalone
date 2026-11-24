package service

import (
	"context"
	"strconv"
	"time"
)

// AdminAccountInfoBinding is an ordinary physical account identity. It carries
// no protected consumer/owner authority and cannot authorize a native route.
type AdminAccountInfoBinding struct {
	AccountID          int64
	CreatedAt          time.Time
	Platform           string
	AccountType        string
	Profile            string
	CredentialRevision int64
	Order              int64
}

// The repository captures a non-secret database revision and source order before
// HTTP, then checks the same physical row and complete credential document when
// accepting the result. Neither credentials nor their digest enter the snapshot.
type AdminAccountInfoRepository interface {
	ReserveAdminAccountInfo(context.Context, *Account) (AdminAccountInfoBinding, error)
	StoreAdminAccountInfo(context.Context, AdminAccountInfoBinding, map[string]any, string, ...GatewayAccountInfoObservation) error
	ReadAdminAccountInfo(context.Context, []*Account) (map[int64]GatewayAccountInfo, error)
}

func AdminAccountInfoProfile(a *Account) string {
	if a == nil || HasGatewayNativeIdentity(a) || a.IsCredentialShadow() {
		return ""
	}
	if a.Type == AccountTypeOAuth {
		switch a.Platform {
		case PlatformOpenAI:
			return GatewayCodexOAuthResponsesProfile
		case PlatformAnthropic:
			return GatewayClaudeOAuthMessagesProfile
		case PlatformGrok:
			return GatewayGrokOAuthResponsesProfile
		case PlatformAntigravity:
			return "antigravity-responses-oauth-v1"
		case PlatformGemini:
			switch a.GeminiOAuthType() {
			case "google_one":
				return "gemini-google-one-responses-oauth-v1"
			case "code_assist":
				return "gemini-code-assist-responses-oauth-v1"
			case "ai_studio":
				return "gemini-ai-studio-responses-oauth-v1"
			}
		}
	}
	return ""
}

// AdminAccountInfoProject uses exactly the protected fact policy/projection.
// Unlike a verified protected principal, an ordinary account may be reauthorized
// as a different upstream subject. No fact survives a credential replacement
// until a trusted producer observes it for the current database revision.
func AdminAccountInfoProject(profile string, stored GatewayAccountInfoStored, now time.Time) GatewayAccountInfo {
	if _, valid := GatewayAccountInfoProject(profile, stored, now); !valid {
		return GatewayAccountInfo{Status: "unavailable"}
	}
	filtered := stored
	filtered.Sources = make(map[string]GatewayAccountInfoObservation, len(stored.Sources))
	for source, observation := range stored.Sources {
		if observation.CredentialRevision == stored.CredentialRevision {
			filtered.Sources[source] = observation
		}
	}
	fields, valid := GatewayAccountInfoProject(profile, filtered, now)
	if !valid {
		return GatewayAccountInfo{Status: "unavailable"}
	}
	return GatewayAccountInfo{Status: "available", InfoRevision: strconv.FormatInt(stored.Revision, 10), Fields: &fields}
}

func populateAdminAccountInfo(ctx context.Context, repo AccountRepository, accounts []*Account) {
	for _, account := range accounts {
		if account != nil && !HasGatewayNativeIdentity(account) {
			info := AdminAccountInfoProject(AdminAccountInfoProfile(account), GatewayAccountInfoStored{}, time.Now())
			account.AccountInfo = &info
		}
	}
	reader, ok := repo.(AdminAccountInfoRepository)
	if !ok || len(accounts) == 0 {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	facts, err := reader.ReadAdminAccountInfo(bounded, accounts)
	for _, account := range accounts {
		if account == nil || HasGatewayNativeIdentity(account) {
			continue
		}
		if err != nil {
			account.AccountInfo = &GatewayAccountInfo{Status: "unavailable"}
		} else if info, present := facts[account.ID]; present {
			account.AccountInfo = &info
		}
	}
}

// Only an explicit authorized create/update input calls this producer. Merely
// reading old tier_id credentials cannot manufacture a declaration timestamp.
func recordAdminDeclaredTierAfterCommit(ctx context.Context, repo AccountRepository, account *Account, input map[string]any) {
	tier, explicit := input["tier_id"].(string)
	if !explicit || account == nil || account.Platform != PlatformGemini || account.Type != AccountTypeOAuth || account.GeminiTierID() != tier {
		return
	}
	writer, ok := repo.(AdminAccountInfoRepository)
	if !ok {
		return
	}
	copy := *account
	credentials, err := cloneAccountJSONMap(account.Credentials)
	if err != nil {
		return
	}
	copy.Credentials = credentials
	AfterAccountCommit(ctx, func(committed context.Context) {
		defer clear(credentials)
		bounded, cancel := context.WithTimeout(committed, 2*time.Second)
		defer cancel()
		binding, err := writer.ReserveAdminAccountInfo(bounded, &copy)
		if err != nil {
			return
		}
		if fields := GatewayDeclaredSubscriptionInfo(binding.Profile, tier, time.Now().UTC()); fields != nil {
			observation := GatewayAccountInfoObservation{Source: "user_declaration", Order: binding.Order,
				CredentialRevision: binding.CredentialRevision, Fields: *fields}
			_ = writer.StoreAdminAccountInfo(bounded, binding, credentials, "", observation)
		}
	})
}
