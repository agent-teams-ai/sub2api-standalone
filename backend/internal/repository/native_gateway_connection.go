package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func nativeConnectionDatabase(r *accountRepository) (interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}, bool) {
	db, ok := r.sql.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	})
	return db, ok
}

func (r *accountRepository) StageGatewayNativeConnection(ctx context.Context, enrollment service.GatewayNativeConnectionEnrollment) (service.GatewayNativePublishedRoute, error) {
	var empty service.GatewayNativePublishedRoute
	in := enrollment.Intent
	if !connectAuthorized(ctx, in) || !service.GatewayNativeConnectionIntent(in) || in.EnrollmentMAC == "" ||
		!service.GatewayConnectionPublicationValid(enrollment.Publication) || enrollment.Publication.Connection != *in.Connection || enrollment.Publication.DispatchFamily != in.NativeDispatchFamily ||
		len(enrollment.Envelope) < 48 || len(enrollment.Envelope) > 90*1024 {
		return empty, service.ErrGatewayNativeIdentity
	}
	db, ok := nativeConnectionDatabase(r)
	if !ok {
		return empty, service.ErrGatewayNativeIdentity
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return empty, service.ErrGatewayNativeIdentity
	}
	defer func() { _ = tx.Rollback() }()
	// Same ordering as cleanup: this existing enrollment lock closes the race
	// between sealing an uncertain operation and a late physical publication.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || chr(1) || $2,245))`, in.Scope.Consumer, in.EnrollmentOperation); err != nil {
		return empty, service.ErrGatewayNativeIdentity
	}
	saved, err := scanConnect(tx.QueryRowContext(ctx, `SELECT `+connectColumns+` FROM gateway_oauth_connect_intents WHERE consumer=$1 AND operation_ref=$2 FOR UPDATE`, in.Scope.Consumer, in.Operation))
	if err != nil || !service.SameGatewayNativeOAuthConnectIntent(in, saved) || saved.EnrollmentMAC != in.EnrollmentMAC || saved.RecoveryDenied {
		return empty, service.ErrGatewayOAuthConflict
	}
	if saved.State == "completed" {
		if saved.NativePublication == nil || *saved.NativePublication != enrollment.Publication {
			return empty, service.ErrGatewayOAuthConflict
		}
		// Completion replay is an immutable receipt, not a fresh custody read.
		// Do not take account after intent: refresh/cleanup lock in the opposite
		// order. The birth cannot be deleted or changed by either path.
		route := service.GatewayNativeRoute{Generation: saved.Scope.Generation, Profile: saved.Connection.ProfileID, BaseURL: saved.NativeMount.BaseURL, Model: saved.NativeMount.Model}
		if tx.QueryRowContext(ctx, `SELECT id,created_at FROM accounts WHERE id=$1
 AND extra->>'gateway_connection_operation_v1'=$2 AND extra->>'gateway_generation_v1'=$3`, saved.Outcome.AccountID, saved.Operation, saved.Scope.Generation).Scan(&route.AccountID, &route.CreatedAt) != nil {
			return empty, service.ErrGatewayNativeIdentity
		}
		if tx.Commit() != nil {
			return empty, service.ErrGatewayNativeIdentity
		}
		return service.GatewayNativePublishedRoute{GatewayNativeRoute: route, Publication: *saved.NativePublication}, nil
	}
	if saved.State != "entered" && saved.State != "unknown" {
		return empty, service.ErrGatewayNativeIdentity
	}
	publication, err := service.GatewayConnectionPublicationCredentials(enrollment.Publication)
	if err != nil {
		return empty, err
	}
	credentials, _ := json.Marshal(map[string]any{
		service.GatewayConnectionBundleCredentialKey:      enrollment.Envelope,
		service.GatewayConnectionRevisionCredentialKey:    "1",
		service.GatewayConnectionPublicationCredentialKey: publication,
	})
	extra, _ := json.Marshal(map[string]any{
		service.GatewayNativeConnectionAccountExtraKey:   true,
		service.GatewayNativeConnectionOperationExtraKey: saved.Operation,
		service.GatewayGenerationExtraKey:                saved.Scope.Generation,
		service.GatewayProfileExtraKey:                   saved.Connection.ProfileID,
		service.GatewayModelExtraKey:                     saved.NativeMount.Model,
		service.GatewayCredentialScopeExtraKey:           saved.Scope.Metadata(),
	})
	var route service.GatewayNativeRoute
	if tx.QueryRowContext(ctx, `INSERT INTO accounts (name,platform,type,status,schedulable,concurrency,priority,credentials,extra)
 VALUES ($1,$2,$3,'disabled',false,1,50,$4::jsonb,$5::jsonb) RETURNING id,created_at`, "Gateway native connection", saved.NativePlatform, saved.NativeAccountType, string(credentials), string(extra)).Scan(&route.AccountID, &route.CreatedAt) != nil {
		return empty, service.ErrGatewayNativeIdentity
	}
	route.Generation, route.Profile, route.BaseURL, route.Model = saved.Scope.Generation, saved.Connection.ProfileID, saved.NativeMount.BaseURL, saved.NativeMount.Model
	pub, _ := service.EncodeGatewayConnectionPublication(enrollment.Publication)
	result, err := tx.ExecContext(ctx, `UPDATE gateway_oauth_connect_intents SET state='completed',material_envelope='',native_publication=$3::jsonb,
 outcome_operation=enrollment_operation,outcome_account_id=$4,outcome_generation=generation,outcome_state='staged'
 WHERE consumer=$1 AND operation_ref=$2 AND state IN ('entered','unknown') AND NOT recovery_denied AND enrollment_mac=$5`, saved.Scope.Consumer, saved.Operation, string(pub), route.AccountID, saved.EnrollmentMAC)
	if err != nil {
		return empty, service.ErrGatewayNativeIdentity
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return empty, service.ErrGatewayOAuthConflict
	}
	physical := service.GatewayNativeConnectionPhysical{Scope: saved.Scope, Operation: saved.Operation, Native: service.GatewayNativePublishedRoute{GatewayNativeRoute: route, Publication: enrollment.Publication}, CredentialVersion: 1}
	observations, err := nativeConnectionAccountInfoObservations(ctx, tx, physical.CredentialVersion, enrollment.Observations)
	if err != nil {
		return empty, err
	}
	for _, observation := range observations {
		if err := storeGatewayAccountInfoTx(ctx, tx, service.GatewayNativeConnectionInfoPhysical(physical), observation, false); err != nil {
			return empty, err
		}
	}
	if tx.Commit() != nil {
		return empty, service.ErrGatewayNativeIdentity
	}
	return physical.Native, nil
}

// Native method ports report provider facts; the journal owns their committed
// credential revision and sequence. The first birth has no concurrent readers,
// and refresh owns the next revision exclusively until this transaction commits.
// Existing producers that already reserved a source order keep that order.
func nativeConnectionAccountInfoObservations(ctx context.Context, tx *sql.Tx, revision int64, input []service.GatewayAccountInfoObservation) ([]service.GatewayAccountInfoObservation, error) {
	if len(input) > service.GatewayAccountInfoMaxSources || revision < 1 {
		return nil, service.ErrGatewayNativeIdentity
	}
	observations := append([]service.GatewayAccountInfoObservation(nil), input...)
	var order int64
	for i := range observations {
		observation := &observations[i]
		if observation.CredentialRevision == 0 {
			observation.CredentialRevision = revision
		}
		if observation.Order == 0 {
			if order == 0 && (tx.QueryRowContext(ctx, `SELECT nextval('gateway_account_info_source_order')`).Scan(&order) != nil || order < 1 || order > 9007199254740991) {
				return nil, service.ErrGatewayNativeIdentity
			}
			observation.Order = order
		}
	}
	return observations, nil
}

func nativeConnectionPhysicalTx(ctx context.Context, tx *sql.Tx, scope service.GatewayNativeCredentialScope, operation string, erased bool) (service.GatewayNativeConnectionPhysical, error) {
	var empty service.GatewayNativeConnectionPhysical
	var id int64
	if tx.QueryRowContext(ctx, `SELECT outcome_account_id FROM gateway_oauth_connect_intents
 WHERE consumer=$1 AND operation_ref=$2 AND owner_ref=$3 AND account_ref=$4 AND generation=$5 AND purpose=$6
 AND client_id='native-connection-v1' AND state='completed' AND NOT recovery_denied`, scope.Consumer, operation, scope.Owner, scope.Account, scope.Generation, scope.Purpose).Scan(&id) != nil || id < 1 {
		return empty, service.ErrGatewayNativeIdentity
	}
	// Lock the physical birth first, then re-read journal facts after any wait.
	if tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 FOR SHARE`, id).Scan(&id) != nil {
		return empty, service.ErrGatewayNativeIdentity
	}
	intent, err := scanConnect(tx.QueryRowContext(ctx, `SELECT `+connectColumns+` FROM gateway_oauth_connect_intents WHERE consumer=$1 AND operation_ref=$2 FOR SHARE`, scope.Consumer, operation))
	if err != nil || intent.Scope != scope || intent.State != "completed" || intent.RecoveryDenied || intent.Outcome.AccountID != id ||
		!service.GatewayNativeConnectionIntent(intent) || !service.GatewayConnectionSelectionAuthorized(ctx, intent.Connection) || intent.NativePublication == nil {
		return empty, service.ErrGatewayNativeIdentity
	}
	a := &service.Account{}
	var credentials, extra []byte
	var deleted bool
	if tx.QueryRowContext(ctx, `SELECT id,created_at,platform,type,status,schedulable,proxy_id,parent_account_id,expires_at,credentials,extra,deleted_at IS NOT NULL
 FROM accounts WHERE id=$1 AND ($2 OR deleted_at IS NULL)
 AND status='disabled' AND NOT schedulable AND proxy_id IS NULL AND parent_account_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM account_groups g WHERE g.account_id=accounts.id)`, id, erased).Scan(&a.ID, &a.CreatedAt, &a.Platform, &a.Type, &a.Status, &a.Schedulable, &a.ProxyID, &a.ParentAccountID, &a.ExpiresAt, &credentials, &extra, &deleted) != nil ||
		json.Unmarshal(credentials, &a.Credentials) != nil || json.Unmarshal(extra, &a.Extra) != nil {
		return empty, service.ErrGatewayNativeIdentity
	}
	publication, err := service.GatewayNativeStoredConnectionPublication(a)
	if err != nil || publication != *intent.NativePublication || a.Platform != intent.NativePlatform || a.Type != intent.NativeAccountType ||
		a.Extra[service.GatewayNativeConnectionAccountExtraKey] != true || a.Extra[service.GatewayNativeConnectionOperationExtraKey] != operation ||
		a.Extra[service.GatewayGenerationExtraKey] != scope.Generation || a.Extra[service.GatewayProfileExtraKey] != intent.Connection.ProfileID || a.Extra[service.GatewayModelExtraKey] != intent.NativeMount.Model {
		return empty, service.ErrGatewayNativeIdentity
	}
	actualScope, err := service.GatewayNativeConnectionStoredScope(a)
	if err != nil || actualScope != scope {
		return empty, service.ErrGatewayNativeIdentity
	}
	revision, err := service.GatewayNativeConnectionCredentialRevision(a)
	if err != nil || (!deleted && a.GetCredential(service.GatewayConnectionBundleCredentialKey) == "") || (deleted && a.GetCredential(service.GatewayConnectionBundleCredentialKey) != "") {
		return empty, service.ErrGatewayNativeIdentity
	}
	route := service.GatewayNativeRoute{AccountID: a.ID, CreatedAt: a.CreatedAt, Generation: scope.Generation, Profile: intent.Connection.ProfileID, BaseURL: intent.NativeMount.BaseURL, Model: intent.NativeMount.Model}
	return service.GatewayNativeConnectionPhysical{Account: a, Scope: scope, Operation: operation,
		Native: service.GatewayNativePublishedRoute{GatewayNativeRoute: route, Publication: publication}, CredentialVersion: revision}, nil
}

func (r *accountRepository) LockGatewayNativeConnection(ctx context.Context, scope service.GatewayNativeCredentialScope, operation string, expected *service.GatewayNativePublishedRoute) (service.GatewayNativeConnectionPhysical, func(), error) {
	var empty service.GatewayNativeConnectionPhysical
	if !service.GatewayConnectionScopeAuthorized(ctx, scope) || !service.GatewayNativeCredentialRefValid(operation) {
		return empty, nil, service.ErrGatewayNativeIdentity
	}
	db, ok := nativeConnectionDatabase(r)
	if !ok {
		return empty, nil, service.ErrGatewayNativeIdentity
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return empty, nil, service.ErrGatewayNativeIdentity
	}
	var once sync.Once
	release := func() { once.Do(func() { _ = tx.Rollback() }) }
	p, err := nativeConnectionPhysicalTx(ctx, tx, scope, operation, false)
	if err != nil || (expected != nil && (expected.Publication != p.Native.Publication || !service.SameGatewayNativeDescriptor(expected.GatewayNativeRoute, p.Native.GatewayNativeRoute))) {
		release()
		return empty, nil, service.ErrGatewayNativeIdentity
	}
	var safe bool
	if tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM gateway_oauth_refresh_attempts WHERE account_id=$1
 AND (state IN ('entered','unknown') OR state='prepared' AND deadline<=clock_timestamp()))`, p.Account.ID).Scan(&safe) != nil || !safe {
		release()
		return empty, nil, service.ErrGatewayNativeIdentity
	}
	return p, release, nil
}

// The canonical resolver never accepts an owner from invocation input. It
// resolves one immutable stored physical birth for the authenticated consumer.
func (r *accountRepository) GatewayNativeConnectionRouteMarked(ctx context.Context, route service.GatewayNativeRoute) (bool, error) {
	if _, err := service.GatewayNativeConsumer(ctx); err != nil || route.AccountID < 1 || route.CreatedAt.IsZero() || route.Generation == "" {
		return false, service.ErrGatewayNativeIdentity
	}
	db, ok := r.sql.(interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	})
	if !ok {
		return false, service.ErrGatewayNativeIdentity
	}
	var marked bool
	if db.QueryRowContext(ctx, `SELECT COALESCE(extra->'gateway_native_connection_v1'='true'::jsonb,false)
 FROM accounts WHERE id=$1 AND created_at=$2 AND deleted_at IS NULL`, route.AccountID, route.CreatedAt).Scan(&marked) != nil {
		return false, service.ErrGatewayNativeIdentity
	}
	// This only chooses the stored decoder. Legacy Claude/Grok rows use their
	// own generation keys; the selected canonical resolver must validate the
	// complete consumer/account/generation/physical tuple before any entry.
	// A marked row with a wrong generation remains marked and cannot fall back.
	return marked, nil
}

func (r *accountRepository) ResolveGatewayNativeConnectionCanonical(ctx context.Context, route service.GatewayNativeRoute, account string) (service.GatewayNativeCredentialScope, string, service.GatewayConnectionPublication, error) {
	var empty service.GatewayNativeCredentialScope
	var noPublication service.GatewayConnectionPublication
	consumer, err := service.GatewayNativeConsumer(ctx)
	if err != nil || !service.GatewayNativeCredentialRefValid(account) || !service.GatewayNativeCredentialRefValid(route.Profile) ||
		route.AccountID < 1 || route.CreatedAt.IsZero() || route.CreatedAt.Nanosecond()%1000 != 0 {
		return empty, "", noPublication, service.ErrGatewayNativeIdentity
	}
	db, ok := r.sql.(interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	})
	if !ok {
		return empty, "", noPublication, service.ErrGatewayNativeIdentity
	}
	var scope service.GatewayNativeCredentialScope
	var operation string
	var rawPublication []byte
	err = db.QueryRowContext(ctx, `SELECT c.owner_ref,c.operation_ref,c.purpose,c.native_publication::text
 FROM gateway_oauth_connect_intents c JOIN accounts a ON a.id=c.outcome_account_id
 WHERE c.consumer=$1 AND c.account_ref=$2 AND c.generation=$3 AND c.client_id='native-connection-v1'
 AND c.state='completed' AND NOT c.recovery_denied
 AND a.id=$4 AND a.created_at=$5 AND a.deleted_at IS NULL
 AND a.status='disabled' AND NOT a.schedulable AND a.extra->>'gateway_connection_operation_v1'=c.operation_ref
 AND c.native_mount->>'baseURL'=$6 AND c.native_mount->>'model'=$7 AND c.connection_binding->>'profileId'=$8
 AND a.credentials->'connection_publication'=c.native_publication
 AND gateway_native_connection_credential_version(a.credentials)>0`, consumer, account, route.Generation, route.AccountID, route.CreatedAt, route.BaseURL, route.Model, route.Profile).Scan(&scope.Owner, &operation, &scope.Purpose, &rawPublication)
	scope.Consumer, scope.Account, scope.Generation = consumer, account, route.Generation
	publication, publicationErr := service.DecodeGatewayConnectionPublication(rawPublication)
	if err != nil || publicationErr != nil || publication.Connection.ProfileID != route.Profile || !service.GatewayNativeOAuthScopeValid(scope) || !service.GatewayNativeCredentialRefValid(operation) {
		return empty, "", noPublication, service.ErrGatewayNativeIdentity
	}
	return scope, operation, publication, nil
}

func (r *accountRepository) ResolveGatewayNativeConnectionRefresh(ctx context.Context, scope service.GatewayNativeCredentialScope, operation string) (service.GatewayNativeConnectionRefreshResolution, error) {
	var out service.GatewayNativeConnectionRefreshResolution
	if !service.GatewayNativeOAuthScopeAuthorized(ctx, scope) || !service.GatewayNativeCredentialRefValid(operation) {
		return out, service.ErrGatewayNativeIdentity
	}
	db, ok := nativeConnectionDatabase(r)
	if !ok {
		return out, service.ErrGatewayNativeIdentity
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return out, service.ErrGatewayNativeIdentity
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	if tx.QueryRowContext(ctx, `SELECT outcome_account_id FROM gateway_oauth_connect_intents
 WHERE consumer=$1 AND operation_ref=$2 AND owner_ref=$3 AND account_ref=$4 AND generation=$5 AND purpose=$6
 AND client_id='native-connection-v1' AND state='completed' AND NOT recovery_denied`, scope.Consumer, operation, scope.Owner, scope.Account, scope.Generation, scope.Purpose).Scan(&id) != nil ||
		tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&id) != nil {
		return out, service.ErrGatewayNativeIdentity
	}
	out.Physical, err = nativeConnectionPhysicalTx(ctx, tx, scope, operation, false)
	if err != nil {
		return service.GatewayNativeConnectionRefreshResolution{}, err
	}
	attempt := service.GatewayNativeOAuthRefreshIntent{Scope: scope, ConnectOperation: operation}
	publication := out.Physical.Native.Publication
	attempt.NativePublication = &publication
	var consumer, owner, account, generation string
	err = tx.QueryRowContext(ctx, `SELECT account_id,native_created_at,consumer,owner_ref,account_ref,generation,
 expected_version,operation_ref,intent_ref,state FROM gateway_oauth_refresh_attempts
 WHERE account_id=$1 ORDER BY expected_version DESC LIMIT 1 FOR UPDATE`, id).Scan(
		&attempt.AccountID, &attempt.CreatedAt, &consumer, &owner, &account, &generation, &attempt.ExpectedVersion, &attempt.Operation, &attempt.Intent, &out.State)
	if err != nil && err != sql.ErrNoRows {
		return service.GatewayNativeConnectionRefreshResolution{}, service.ErrGatewayNativeIdentity
	}
	if err == nil {
		if consumer != scope.Consumer || owner != scope.Owner || account != scope.Account || generation != scope.Generation || attempt.AccountID != id ||
			!attempt.CreatedAt.Equal(out.Physical.Native.CreatedAt) || !attempt.Authorized(ctx) ||
			(out.State == "completed" && attempt.ExpectedVersion+1 != out.Physical.CredentialVersion) || (out.State != "completed" && attempt.ExpectedVersion != out.Physical.CredentialVersion) {
			return service.GatewayNativeConnectionRefreshResolution{}, service.ErrGatewayNativeIdentity
		}
		out.Attempt = &attempt
	}
	if tx.Commit() != nil {
		return service.GatewayNativeConnectionRefreshResolution{}, service.ErrGatewayNativeIdentity
	}
	return out, nil
}

var _ service.GatewayNativeConnectionRepository = (*accountRepository)(nil)
