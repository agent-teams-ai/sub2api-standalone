//go:build integration || gatewaycustody

package repository

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type adminInfoTransport func(*http.Request) (*http.Response, error)

func (f adminInfoTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAdminAccountInfoPostgresTrustedProducerAndCredentialFence(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_ADMIN_INFO_TEST_DSN", "admin_info_test_")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	adminRepo, ok := accounts.(service.AdminAccountRepository)
	require.True(t, ok)
	infoRepo, ok := accounts.(service.AdminAccountInfoRepository)
	require.True(t, ok)
	provider := uuid.NewString()
	credentials := map[string]any{"access_token": "admin-original-private-token", "chatgpt_account_id": provider,
		"plan_type": "untrusted-plan", "subscription_expires_at": "2099-01-01T00:00:00Z"}
	raw, err := json.Marshal(credentials)
	require.NoError(t, err)
	var id int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,extra)
 VALUES('admin info fixture','openai','oauth',$1::jsonb,'{"privacy_mode":"training_off","subscription_tier":"untrusted-extra"}'::jsonb) RETURNING id`, string(raw)).Scan(&id))
	var requests, patches, phase atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		require.Equal(t, "chatgpt.com", r.Host)
		require.Contains(t, []string{"Bearer admin-original-private-token", "Bearer admin-replacement-private-token"}, r.Header.Get("Authorization"))
		var reserved bool
		require.NoError(t, db.QueryRowContext(ctx, `SELECT is_called FROM gateway_account_info_source_order`).Scan(&reserved))
		require.True(t, reserved, "source order is reserved before either authenticated HTTP request")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/accounts/check/v4-2023-04-27":
			require.Equal(t, http.MethodGet, r.Method)
			_ = json.NewEncoder(w).Encode(map[string]any{"accounts": map[string]any{"map-key-is-not-identity": map[string]any{
				"account": map[string]any{"account_id": provider, "is_default": true, "plan_type": "plus", "account_structure": "personal"}}}})
		case "/backend-api/subscriptions":
			require.Equal(t, provider, r.URL.Query().Get("account_id"))
			_ = json.NewEncoder(w).Encode(map[string]any{"account_id": provider, "active_until": "2028-01-02T05:00:00+05:00"})
		case "/backend-api/settings/account_user_setting":
			patches.Add(1)
			require.Equal(t, http.MethodPatch, r.Method)
			require.Equal(t, "feature=training_allowed&value=false", r.URL.RawQuery)
			switch phase.Load() {
			case 1:
				w.Header().Set("cf-mitigated", "challenge")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("Cloudflare challenge"))
			case 2:
				_, updateErr := db.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(credentials,'{access_token}','"admin-replacement-private-token"'::jsonb) WHERE id=$1`, id)
				require.NoError(t, updateErr)
				w.WriteHeader(http.StatusNoContent)
			default:
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			t.Errorf("unexpected metadata endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	transport, ok := server.Client().Transport.(*http.Transport)
	require.True(t, ok)
	factory := func(string) (*req.Client, error) {
		cfg := transport.TLSClientConfig.Clone()
		cfg.ServerName = "example.com"
		return req.C().SetProxy(nil).SetTLSClientConfig(cfg).SetDial(func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "chatgpt.com:443" {
				return nil, service.ErrGatewayNativeIdentity
			}
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
		}), nil
	}
	admin := service.NewAdminService(nil, nil, NewAdminGroupRepository(accounts.(*accountRepository).client, db), adminRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, factory, nil, nil, nil, nil, nil, nil)
	account, err := admin.GetAccount(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, account.AccountInfo)
	require.Equal(t, "unknown", account.AccountInfo.Fields.TrainingUsage.Status, "raw imported success is not a fact")
	require.Equal(t, "unknown", account.AccountInfo.Fields.Subscription.Status)
	require.Zero(t, requests.Load(), "admin read does no provider I/O")

	require.Equal(t, service.PrivacyModeTrainingOff, admin.EnsureOpenAIPrivacy(ctx, account))
	require.EqualValues(t, 1, patches.Load(), "unproven legacy marker cannot suppress the trusted producer")
	account, err = admin.GetAccount(ctx, id)
	require.NoError(t, err)
	fields := account.AccountInfo.Fields
	require.Equal(t, "known", fields.TrainingUsage.Status)
	require.Equal(t, "disabled", *fields.TrainingUsage.Value)
	require.Equal(t, "provider_api", fields.TrainingUsage.Source)
	require.NotEmpty(t, fields.TrainingUsage.ObservedAt)
	require.Equal(t, "plus", fields.Subscription.Value.Code)
	require.Equal(t, "2028-01-02T00:00:00Z", fields.Subscription.Value.EndsAt.Value, "admin uses the same canonical expiry helper")
	protectedProjection := service.ProjectGatewayAccountInfoV1(service.GatewayAccountInfoResult{Info: *account.AccountInfo})
	require.Equal(t, "unsupported", protectedProjection.Info.Fields.PrivacyMode.Status)
	full := dto.AccountFromService(account)
	lite := dto.AccountListItemFromAccount(full)
	require.Equal(t, account.AccountInfo, full.AccountInfo)
	require.Equal(t, account.AccountInfo, lite.AccountInfo)
	display, err := json.Marshal(lite.AccountInfo)
	require.NoError(t, err)
	require.NotContains(t, string(display), "private-token")
	require.NotContains(t, string(display), "untrusted")

	beforeReads := requests.Load()
	_, err = admin.GetAccountsByIDs(ctx, []int64{id})
	require.NoError(t, err)
	_, _, err = admin.ListAccounts(ctx, 1, 10, "", "", "", "", 0, "", "", "")
	require.NoError(t, err)
	require.Empty(t, admin.EnsureOpenAIPrivacy(ctx, account), "current confirmed fact suppresses unnecessary PATCH")
	require.Equal(t, beforeReads, requests.Load())

	phase.Store(1)
	require.Equal(t, service.PrivacyModeCFBlocked, admin.ForceOpenAIPrivacy(ctx, account))
	failed, err := admin.GetAccount(ctx, id)
	require.NoError(t, err)
	require.Equal(t, service.PrivacyModeCFBlocked, failed.Extra["privacy_mode"])
	require.Equal(t, fields.TrainingUsage, failed.AccountInfo.Fields.TrainingUsage, "PATCH failure does not clear or forge a training fact")

	phase.Store(2)
	require.Equal(t, service.PrivacyModeTrainingOff, admin.ForceOpenAIPrivacy(ctx, failed))
	staleFacts, staleErr := infoRepo.ReadAdminAccountInfo(ctx, []*service.Account{failed})
	require.NoError(t, staleErr)
	require.Equal(t, "unavailable", staleFacts[id].Status, "batch projection cannot attach facts to an older credential document")
	replaced, err := admin.GetAccount(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "admin-replacement-private-token", replaced.GetCredential("access_token"))
	require.Equal(t, "unknown", replaced.AccountInfo.Fields.TrainingUsage.Status, "late success cannot bind to replacement credentials")
	require.Equal(t, "unknown", replaced.AccountInfo.Fields.Subscription.Status)
	require.Equal(t, service.PrivacyModeCFBlocked, replaced.Extra["privacy_mode"], "late diagnostics cannot overwrite the new credential generation either")

	phase.Store(0)
	require.Equal(t, service.PrivacyModeTrainingOff, admin.EnsureOpenAIPrivacy(ctx, replaced))
	current, err := admin.GetAccount(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "known", current.AccountInfo.Fields.TrainingUsage.Status)
	var revision int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT account_info_credential_revision FROM accounts WHERE id=$1`, id).Scan(&revision))
	require.EqualValues(t, 2, revision, "diagnostics and read projection do not advance credential revision")
	_, err = db.ExecContext(ctx, `UPDATE accounts SET account_info_credential_revision=1,name='same identity' WHERE id=$1`, id)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT account_info_credential_revision FROM accounts WHERE id=$1`, id).Scan(&revision))
	require.EqualValues(t, 2, revision, "revision is database owned even on ordinary administrative updates")

	binding, err := infoRepo.ReserveAdminAccountInfo(ctx, current)
	require.NoError(t, err)
	invalid := service.GatewayAccountInfoObservation{Source: "training_settings", Order: binding.Order, CredentialRevision: binding.CredentialRevision,
		Fields: service.GatewayAccountInfoEmptyFields(binding.Profile)}
	invalid.Fields.TrainingUsage = service.GatewayAccountFact[string]{Status: "known"}
	require.Error(t, infoRepo.StoreAdminAccountInfo(ctx, binding, current.Credentials, "", invalid))
	afterFailure, err := admin.GetAccount(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "admin-replacement-private-token", afterFailure.GetCredential("access_token"), "optional metadata failure cannot discard a committed rotation")
	require.Equal(t, current.AccountInfo, afterFailure.AccountInfo)

	t.Run("explicit Gemini tier stays declared", func(t *testing.T) {
		gemini, createErr := admin.CreateAccount(ctx, &service.CreateAccountInput{Name: "declared Gemini", Platform: service.PlatformGemini,
			Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "gemini-private", "oauth_type": "google_one", "tier_id": "pro"}, Concurrency: 1, Priority: 1})
		require.NoError(t, createErr)
		read, readErr := admin.GetAccount(ctx, gemini.ID)
		require.NoError(t, readErr)
		require.Equal(t, "known", read.AccountInfo.Fields.Subscription.Status)
		require.Equal(t, "user_supplied", read.AccountInfo.Fields.Subscription.Source)
		require.Equal(t, "pro", read.AccountInfo.Fields.Subscription.Value.Code)
		require.NotEmpty(t, read.AccountInfo.Fields.Subscription.ObservedAt)
		_, updateErr := db.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(credentials,'{tier_id}','"ultra"'::jsonb) WHERE id=$1`, gemini.ID)
		require.NoError(t, updateErr)
		read, readErr = admin.GetAccount(ctx, gemini.ID)
		require.NoError(t, readErr)
		require.Equal(t, "unknown", read.AccountInfo.Fields.Subscription.Status, "stored tiers have no synthetic declaration time")
		// The existing edit contract is a full non-sensitive credential PUT;
		// only omitted sensitive fields are preserved by the account service.
		_, updateErr = admin.UpdateAccount(ctx, gemini.ID, &service.UpdateAccountInput{Credentials: map[string]any{"oauth_type": "google_one", "tier_id": "ultra"}})
		require.NoError(t, updateErr)
		read, readErr = admin.GetAccount(ctx, gemini.ID)
		require.NoError(t, readErr)
		require.Equal(t, "google_one", read.GeminiOAuthType())
		require.Equal(t, "gemini-private", read.GetCredential("access_token"))
		require.Equal(t, "user_supplied", read.AccountInfo.Fields.Subscription.Source)
		require.Equal(t, "ultra", read.AccountInfo.Fields.Subscription.Value.Code)
		oldBinding, bindErr := infoRepo.ReserveAdminAccountInfo(ctx, read)
		require.NoError(t, bindErr)
		_, updateErr = db.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(credentials,'{oauth_type}','"code_assist"'::jsonb) WHERE id=$1`, gemini.ID)
		require.NoError(t, updateErr)
		staleRead, staleErr := infoRepo.ReadAdminAccountInfo(ctx, []*service.Account{read})
		require.NoError(t, staleErr)
		require.Equal(t, "unavailable", staleRead[gemini.ID].Status, "batch projection fences a concurrent OAuth subtype change")
		require.Error(t, infoRepo.StoreAdminAccountInfo(ctx, oldBinding, read.Credentials, ""))
		partial, updateErr := admin.UpdateAccount(ctx, gemini.ID, &service.UpdateAccountInput{Credentials: map[string]any{"tier_id": "partial-tier"}})
		require.NoError(t, updateErr)
		require.Empty(t, partial.GeminiOAuthType(), "a partial credential PUT removes the omitted non-sensitive subtype")
		require.Equal(t, "gemini-private", partial.GetCredential("access_token"))
		require.Equal(t, "unsupported", partial.AccountInfo.Fields.Subscription.Status, "an unknown subtype cannot gain a declared subscription fact")
		require.Empty(t, partial.AccountInfo.Fields.Subscription.Source)
	})

	t.Run("Antigravity keeps fresh provider tiers distinct from imported plans", func(t *testing.T) {
		var antigravityID int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,extra)
 VALUES('Antigravity facts','antigravity','oauth','{"access_token":"ag-private-1","project_id":"observed-project","plan_type":"untrusted-Ultra"}'::jsonb,
 '{"privacy_mode":"privacy_set"}'::jsonb) RETURNING id`).Scan(&antigravityID))
		var calls, agPhase atomic.Int32
		agServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			require.Equal(t, http.MethodPost, r.Method)
			require.Equal(t, "/v1internal:loadCodeAssist", r.URL.Path)
			require.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ag-private-"))
			var order int64
			require.NoError(t, db.QueryRowContext(ctx, `SELECT last_value FROM gateway_account_info_source_order`).Scan(&order))
			require.Positive(t, order)
			if agPhase.Load() == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if agPhase.Load() == 2 {
				_, updateErr := db.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(credentials,'{access_token}','"ag-private-replaced"'::jsonb) WHERE id=$1`, antigravityID)
				require.NoError(t, updateErr)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":"observed-project","currentTier":{"id":"free-tier"},"paidTier":{"id":"g1-pro-tier"}}`))
		}))
		defer agServer.Close()
		agTransport := agServer.Client().Transport.(*http.Transport).Clone()
		agTransport.Proxy = nil
		agTransport.TLSClientConfig.ServerName = "example.com"
		agTransport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, agServer.Listener.Addr().String())
		}
		previousTransport := http.DefaultTransport
		http.DefaultTransport = adminInfoTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Scheme != "https" || r.URL.Host != "cloudcode-pa.googleapis.com" && r.URL.Host != "daily-cloudcode-pa.googleapis.com" {
				return nil, service.ErrGatewayNativeIdentity
			}
			return agTransport.RoundTrip(r)
		})
		defer func() { http.DefaultTransport = previousTransport; agTransport.CloseIdleConnections() }()
		read, readErr := admin.GetAccount(ctx, antigravityID)
		require.NoError(t, readErr)
		require.Equal(t, "unknown", read.AccountInfo.Fields.Subscription.Status)
		require.Zero(t, calls.Load())
		require.Equal(t, service.AntigravityPrivacySet, admin.EnsureAntigravityPrivacy(ctx, read))
		read, readErr = admin.GetAccount(ctx, antigravityID)
		require.NoError(t, readErr)
		require.NotZero(t, calls.Load())
		require.Equal(t, "provider_api", read.AccountInfo.Fields.Subscription.Source)
		require.Equal(t, "g1-pro-tier", read.AccountInfo.Fields.Subscription.Value.Code)
		require.Equal(t, "Pro", read.AccountInfo.Fields.Subscription.Value.Label)
		require.Nil(t, read.AccountInfo.Fields.Subscription.Value.EndsAt)
		require.Equal(t, "unsupported", read.AccountInfo.Fields.TrainingUsage.Status, "legacy privacy_set is not a training fact")
		before := calls.Load()
		_, readErr = admin.GetAccount(ctx, antigravityID)
		require.NoError(t, readErr)
		require.Equal(t, service.AntigravityPrivacySet, admin.EnsureAntigravityPrivacy(ctx, read))
		require.Equal(t, before, calls.Load(), "current canonical info and admin readers do not query the provider again")
		_, updateErr := db.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(credentials,'{access_token}','"ag-private-2"'::jsonb) WHERE id=$1`, antigravityID)
		require.NoError(t, updateErr)
		read, readErr = admin.GetAccount(ctx, antigravityID)
		require.NoError(t, readErr)
		agPhase.Store(1)
		_ = admin.EnsureAntigravityPrivacy(ctx, read)
		read, readErr = admin.GetAccount(ctx, antigravityID)
		require.NoError(t, readErr)
		require.Equal(t, "unknown", read.AccountInfo.Fields.Subscription.Status, "failed observation cannot revive a prior credential's tier")
		agPhase.Store(2)
		_ = admin.EnsureAntigravityPrivacy(ctx, read)
		read, readErr = admin.GetAccount(ctx, antigravityID)
		require.NoError(t, readErr)
		require.Equal(t, "ag-private-replaced", read.GetCredential("access_token"))
		require.Equal(t, "unknown", read.AccountInfo.Fields.Subscription.Status, "late provider reply cannot cross the credential CAS")
	})

	t.Run("Grok billing facts use only current authenticated replies", func(t *testing.T) {
		grokCredentials, marshalErr := json.Marshal(map[string]any{"access_token": "grok-private", "refresh_token": "grok-refresh", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		require.NoError(t, marshalErr)
		var grokID int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,extra) VALUES('Grok facts','grok','oauth',$1::jsonb,'{"grok_billing_snapshot":{"plan":"untrusted-plan","monthly_limit_cents":99999999}}'::jsonb) RETURNING id`, string(grokCredentials)).Scan(&grokID))
		var billingPhase atomic.Int32
		billingServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "cli-chat-proxy.grok.com", r.Host)
			require.Equal(t, "/v1/billing", r.URL.Path)
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "Bearer grok-private", r.Header.Get("Authorization"))
			if billingPhase.Load() == 1 {
				if r.URL.RawQuery == "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				_, _ = w.Write([]byte(`{"config":{"creditUsagePercent":3}}`))
				return
			}
			if billingPhase.Load() == 2 {
				_, rotateErr := db.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(credentials,'{access_token}','"grok-replacement"'::jsonb) WHERE id=$1`, grokID)
				require.NoError(t, rotateErr)
			}
			_, _ = w.Write([]byte(`{"config":{"monthlyLimit":{"val":15000},"used":{"val":75},"billingPeriodEnd":"2028-01-01T00:00:00Z"}}`))
		}))
		defer billingServer.Close()
		base := billingServer.Client().Transport.(*http.Transport)
		transport := base.Clone()
		transport.TLSClientConfig.ServerName = "example.com"
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "cli-chat-proxy.grok.com:443" {
				return nil, service.ErrGatewayNativeIdentity
			}
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, billingServer.Listener.Addr().String())
		}
		defer transport.CloseIdleConnections()
		probe := service.NewGrokQuotaService(accounts, nil, service.NewGrokTokenProvider(accounts, nil), &nativeConnectionTLSUpstream{client: &http.Client{Transport: transport}}, &config.Config{})
		read, readErr := admin.GetAccount(ctx, grokID)
		require.NoError(t, readErr)
		require.Equal(t, "unknown", read.AccountInfo.Fields.Subscription.Status)
		_, probeErr := probe.ProbeBilling(ctx, grokID)
		require.NoError(t, probeErr)
		read, readErr = admin.GetAccount(ctx, grokID)
		require.NoError(t, readErr)
		fact := read.AccountInfo.Fields.Subscription
		require.Equal(t, "known", fact.Status)
		require.Equal(t, "inferred", fact.Source)
		require.NotEqual(t, "untrusted-plan", fact.Value.Code)
		require.Nil(t, fact.Value.EndsAt, "billing period end is not subscription expiry")
		billingPhase.Store(1)
		_, probeErr = probe.ProbeBilling(ctx, grokID)
		require.NoError(t, probeErr)
		read, readErr = admin.GetAccount(ctx, grokID)
		require.NoError(t, readErr)
		require.Equal(t, fact, read.AccountInfo.Fields.Subscription, "partial failure cannot mint a new observation from merged old extras")
		billingPhase.Store(2)
		_, probeErr = probe.ProbeBilling(ctx, grokID)
		require.NoError(t, probeErr)
		read, readErr = admin.GetAccount(ctx, grokID)
		require.NoError(t, readErr)
		require.Equal(t, "unknown", read.AccountInfo.Fields.Subscription.Status, "late billing cannot survive credential replacement")
	})
	_, err = db.ExecContext(ctx, `UPDATE accounts SET deleted_at=clock_timestamp() WHERE id=$1`, id)
	require.NoError(t, err)
	var snapshots int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM gateway_account_info_snapshots WHERE account_id=$1`, id).Scan(&snapshots))
	require.Zero(t, snapshots)
	require.Error(t, infoRepo.StoreAdminAccountInfo(ctx, binding, current.Credentials, service.PrivacyModeTrainingOff))
}
