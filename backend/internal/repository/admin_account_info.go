package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// The explicit domain is persisted as subject_kind, not disguised as a native
// owner. The digest covers only public physical identity, never credential bytes.
func adminAccountInfoStorageIdentity(b service.AdminAccountInfoBinding) accountInfoStorageIdentity {
	raw, _ := json.Marshal([]any{"ordinary-admin-account-info/v1", b.AccountID, b.CreatedAt.UTC().Format(time.RFC3339Nano), b.Platform, b.AccountType})
	digest := sha256.Sum256(raw)
	return accountInfoStorageIdentity{kind: "admin", generation: "admin", principal: hex.EncodeToString(digest[:]),
		profile: b.Profile, accountID: b.AccountID, credentialRevision: b.CredentialRevision, createdAt: b.CreatedAt}
}

func (r *accountRepository) ReserveAdminAccountInfo(ctx context.Context, expected *service.Account) (service.AdminAccountInfoBinding, error) {
	var binding service.AdminAccountInfoBinding
	if r == nil || r.sql == nil || service.AdminAccountInfoProfile(expected) == "" || expected.ID < 1 || expected.CreatedAt.IsZero() {
		return binding, service.ErrGatewayNativeIdentity
	}
	raw, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return binding, service.ErrGatewayNativeIdentity
	}
	db, ok := r.sql.(*sql.DB)
	if !ok {
		return binding, service.ErrGatewayNativeIdentity
	}
	var extra []byte
	var parent sql.NullInt64
	err = db.QueryRowContext(ctx, `SELECT id,created_at,platform,type,account_info_credential_revision,extra,parent_account_id,
 nextval('gateway_account_info_source_order') FROM accounts
 WHERE id=$1 AND created_at=$2 AND credentials=$3::jsonb AND deleted_at IS NULL`, expected.ID, expected.CreatedAt, string(raw)).Scan(
		&binding.AccountID, &binding.CreatedAt, &binding.Platform, &binding.AccountType, &binding.CredentialRevision, &extra, &parent, &binding.Order)
	var current service.Account
	if err != nil || json.Unmarshal(extra, &current.Extra) != nil || parent.Valid || service.HasGatewayNativeIdentity(&current) ||
		binding.Platform != expected.Platform || binding.AccountType != expected.Type || binding.Order < 1 || binding.CredentialRevision < 1 {
		return service.AdminAccountInfoBinding{}, service.ErrGatewayNativeIdentity
	}
	binding.Profile = service.AdminAccountInfoProfile(expected)
	return binding, nil
}

// StoreAdminAccountInfo never changes credentials. Refresh has already committed
// its replacement before this optional metadata step, so a metadata failure
// cannot discard the successfully rotated token or cause an exchange replay.
func (r *accountRepository) StoreAdminAccountInfo(ctx context.Context, b service.AdminAccountInfoBinding, expectedCredentials map[string]any, privacyMode string, observations ...service.GatewayAccountInfoObservation) error {
	if r == nil {
		return service.ErrGatewayNativeIdentity
	}
	db, ok := r.sql.(*sql.DB)
	if !ok || b.AccountID < 1 || b.Order < 1 || b.CredentialRevision < 1 || len(observations) > service.GatewayAccountInfoMaxSources {
		return service.ErrGatewayNativeIdentity
	}
	switch privacyMode {
	case "", service.PrivacyModeTrainingOff, service.PrivacyModeFailed, service.PrivacyModeCFBlocked:
	default:
		return service.ErrGatewayNativeIdentity
	}
	raw, err := json.Marshal(normalizeJSONMap(expectedCredentials))
	if err != nil {
		return service.ErrGatewayNativeIdentity
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var extra []byte
	var current service.Account
	var parent sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT platform,type,extra,parent_account_id FROM accounts
 WHERE id=$1 AND created_at=$2 AND account_info_credential_revision=$3 AND credentials=$4::jsonb AND deleted_at IS NULL FOR UPDATE`,
		b.AccountID, b.CreatedAt, b.CredentialRevision, string(raw)).Scan(&current.Platform, &current.Type, &extra, &parent)
	if err != nil || json.Unmarshal(extra, &current.Extra) != nil || json.Unmarshal(raw, &current.Credentials) != nil || parent.Valid || service.HasGatewayNativeIdentity(&current) ||
		current.Platform != b.Platform || current.Type != b.AccountType || service.AdminAccountInfoProfile(&current) != b.Profile {
		return service.ErrGatewayNativeIdentity
	}
	key := adminAccountInfoStorageIdentity(b)
	seen := make(map[string]bool, len(observations))
	for _, observation := range observations {
		if observation.Order != b.Order || observation.CredentialRevision != b.CredentialRevision || seen[observation.Source] {
			return service.ErrGatewayNativeIdentity
		}
		seen[observation.Source] = true
		if err := storeAccountInfoTx(ctx, tx, key, observation, false); err != nil {
			return err
		}
	}
	if privacyMode != "" {
		// Operational PATCH outcome stays separate from a confirmed training fact.
		_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{privacy_mode}',to_jsonb($2::text)),updated_at=NOW() WHERE id=$1`, b.AccountID, privacyMode)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// One bounded batch query supplies the admin list. It performs no upstream I/O
// and does not broaden the admin service's protected-account visibility fence.
func (r *accountRepository) ReadAdminAccountInfo(ctx context.Context, accounts []*service.Account) (map[int64]service.GatewayAccountInfo, error) {
	out := make(map[int64]service.GatewayAccountInfo, len(accounts))
	expected := make(map[int64]*service.Account, len(accounts))
	expectedCredentials := make(map[string]map[string]any, len(accounts))
	ids := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		if account == nil || service.HasGatewayNativeIdentity(account) || service.AdminAccountInfoProfile(account) == "" {
			continue
		}
		expected[account.ID] = account
		expectedCredentials[strconv.FormatInt(account.ID, 10)] = normalizeJSONMap(account.Credentials)
		ids = append(ids, account.ID)
	}
	if len(ids) == 0 {
		return out, nil
	}
	credentialsJSON, err := json.Marshal(expectedCredentials)
	if err != nil {
		return nil, service.ErrGatewayNativeIdentity
	}
	defer clear(credentialsJSON)
	rows, err := r.sql.QueryContext(ctx, `SELECT a.id,a.created_at,a.platform,a.type,a.account_info_credential_revision,a.extra,a.parent_account_id,
 COALESCE(a.credentials->>'oauth_type',''),COALESCE(a.credentials->>'project_id',''),a.credentials=($2::jsonb->a.id::text),
 COALESCE(s.principal_version,''),COALESCE(s.revision,0),COALESCE(s.sources,'{}'::jsonb)
 FROM accounts a LEFT JOIN gateway_account_info_snapshots s ON s.account_id=a.id AND s.native_created_at=a.created_at
 AND s.subject_kind='admin' AND s.generation='admin' WHERE a.id=ANY($1) AND a.deleted_at IS NULL`, pq.Array(ids), string(credentialsJSON))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var b service.AdminAccountInfoBinding
		var current service.Account
		var parent sql.NullInt64
		var extra, raw []byte
		var oauthType, projectID string
		var credentialsMatch bool
		stored := service.GatewayAccountInfoStored{}
		if err := rows.Scan(&b.AccountID, &b.CreatedAt, &b.Platform, &b.AccountType, &b.CredentialRevision, &extra, &parent, &oauthType, &projectID, &credentialsMatch,
			&stored.PrincipalVersion, &stored.Revision, &raw); err != nil {
			return nil, err
		}
		a := expected[b.AccountID]
		if a == nil || !credentialsMatch || !a.CreatedAt.Equal(b.CreatedAt) || a.Platform != b.Platform || a.Type != b.AccountType || parent.Valid ||
			json.Unmarshal(extra, &current.Extra) != nil || service.HasGatewayNativeIdentity(&current) {
			out[b.AccountID] = service.GatewayAccountInfo{Status: "unavailable"}
			continue
		}
		current.Platform, current.Type = b.Platform, b.AccountType
		current.Credentials = map[string]any{"oauth_type": oauthType, "project_id": projectID}
		b.Profile = service.AdminAccountInfoProfile(&current)
		if b.Profile == "" || b.Profile != service.AdminAccountInfoProfile(a) {
			out[b.AccountID] = service.GatewayAccountInfo{Status: "unavailable"}
			continue
		}
		key := adminAccountInfoStorageIdentity(b)
		stored.CredentialRevision = b.CredentialRevision
		if stored.Revision == 0 || stored.PrincipalVersion != key.principal {
			stored = service.GatewayAccountInfoStored{CredentialRevision: b.CredentialRevision}
		} else if len(raw) > 32768 || json.Unmarshal(raw, &stored.Sources) != nil || stored.Sources == nil {
			out[b.AccountID] = service.GatewayAccountInfo{Status: "unavailable"}
			continue
		}
		out[b.AccountID] = service.AdminAccountInfoProject(b.Profile, stored, time.Now())
	}
	return out, rows.Err()
}
