-- Native methods share the existing one-use connect and fenced refresh
-- journals. Provider credentials still have one home: protected accounts.
CREATE FUNCTION gateway_native_connection_publication_valid(v jsonb)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT jsonb_typeof(v)='object'
  AND v ?& ARRAY['connection','credential','dispatchFamily','authFamily']
  AND v - ARRAY['connection','credential','dispatchFamily','authFamily']='{}'::jsonb
  AND gateway_native_connection_binding_valid(v->'connection') IS TRUE
  AND jsonb_typeof(v->'credential')='object'
  AND v->'credential' ?& ARRAY['credentialKind','credentialPolicy','refreshPolicy']
  AND (v->'credential') - ARRAY['credentialKind','credentialPolicy','refreshPolicy']='{}'::jsonb
  AND v->'credential'->>'credentialKind' IN ('api_key','oauth','native')
  AND v->'credential'->>'credentialPolicy' IN ('provider_key','subscription_session','cloud_identity','delegated_identity')
  AND v->'credential'->>'refreshPolicy' IN ('native','none')
  AND jsonb_typeof(v->'dispatchFamily')='string' AND v->>'dispatchFamily' ~ '^[a-z][a-z0-9_-]{0,127}$'
  AND jsonb_typeof(v->'authFamily')='string' AND v->>'authFamily' ~ '^[a-z][a-z0-9_-]{0,127}$';
$$;

CREATE FUNCTION gateway_native_connection_mount_valid(v jsonb)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT jsonb_typeof(v)='object' AND v ?& ARRAY['baseURL','model','qualificationRef']
  AND v - ARRAY['baseURL','model','qualificationRef']='{}'::jsonb
  AND jsonb_typeof(v->'baseURL')='string' AND octet_length(v->>'baseURL') BETWEEN 1 AND 2048
  AND v->>'baseURL' ~ '^https?://[^/?#@[:space:]]+(/[^?#[:space:]]*)?$'
  AND jsonb_typeof(v->'model')='string' AND octet_length(v->>'model') BETWEEN 1 AND 1024
  AND v->>'model'=btrim(v->>'model') AND v->>'model' !~ '[[:cntrl:]]'
  AND jsonb_typeof(v->'qualificationRef')='string' AND v->>'qualificationRef' ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$';
$$;

CREATE FUNCTION gateway_native_connection_credential_version(v jsonb)
RETURNS bigint LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE parts text[]; version bigint;
BEGIN
 IF jsonb_typeof(v) IS DISTINCT FROM 'object'
  OR v - ARRAY['connection_bundle','connection_publication','connection_revision'] <> '{}'::jsonb
  OR gateway_native_connection_publication_valid(v->'connection_publication') IS DISTINCT FROM TRUE
  OR jsonb_typeof(v->'connection_revision') IS DISTINCT FROM 'string'
  OR v->>'connection_revision' !~ '^[1-9][0-9]{0,18}$'
  OR jsonb_typeof(v->'connection_bundle') IS DISTINCT FROM 'string'
  OR octet_length(v->>'connection_bundle') NOT BETWEEN 48 AND 92160 THEN RETURN 0; END IF;
 version := (v->>'connection_revision')::bigint;
 parts := string_to_array(v->>'connection_bundle','.');
 IF array_length(parts,1)<>4 OR parts[1]<>'gcb1' OR parts[2] !~ '^[A-Za-z0-9_-]{1,64}$'
  OR parts[3] !~ '^[A-Za-z0-9_-]{16}$' OR parts[4] !~ '^[A-Za-z0-9_-]+$' THEN RETURN 0; END IF;
 RETURN version;
EXCEPTION WHEN OTHERS THEN RETURN 0;
END;
$$;

ALTER TABLE gateway_oauth_connect_intents
 ADD COLUMN native_mount jsonb,
 ADD COLUMN native_dispatch_family text,
 ADD COLUMN native_platform text,
 ADD COLUMN native_account_type text,
 ADD COLUMN native_publication jsonb;

ALTER TABLE gateway_oauth_connect_intents DROP CONSTRAINT gateway_oauth_connect_intents_client_id_check;
ALTER TABLE gateway_oauth_connect_intents ADD CONSTRAINT gateway_oauth_connect_client CHECK
 (client_id IN ('app_EMoamEEZ73f0CkXaXp7hrann','native-connection-v1'));
ALTER TABLE gateway_oauth_connect_intents DROP CONSTRAINT gateway_oauth_connect_method;
ALTER TABLE gateway_oauth_connect_intents ADD CONSTRAINT gateway_oauth_connect_method CHECK
 (method IN ('browser','device_code','refresh_token','auth_json','native'));
ALTER TABLE gateway_oauth_connect_intents DROP CONSTRAINT gateway_oauth_connect_redirect_method;
ALTER TABLE gateway_oauth_connect_intents ADD CONSTRAINT gateway_oauth_connect_redirect_method CHECK (
 (method='browser' AND redirect_uri='http://localhost:1455/auth/callback')
 OR (method='device_code' AND redirect_uri='https://auth.openai.com/deviceauth/callback')
 OR (method IN ('refresh_token','auth_json','native') AND redirect_uri=''));
ALTER TABLE gateway_oauth_connect_intents DROP CONSTRAINT gateway_oauth_connect_import_shape;
ALTER TABLE gateway_oauth_connect_intents ADD CONSTRAINT gateway_oauth_connect_import_shape CHECK (
 (method IN ('refresh_token','auth_json','native') AND import_input_mac IS NOT NULL AND import_claim_mac IS NOT NULL AND import_key_commitment IS NOT NULL)
 OR (method IN ('browser','device_code') AND import_input_mac IS NULL AND import_claim_mac IS NULL AND import_key_commitment IS NULL));
ALTER TABLE gateway_oauth_connect_intents DROP CONSTRAINT gateway_oauth_connect_material_shape;
ALTER TABLE gateway_oauth_connect_intents ADD CONSTRAINT gateway_oauth_connect_material_shape CHECK (
 (state IN ('prepared','entered') AND (
   (method IN ('browser','device_code') AND material_envelope ~ '^gcc1\.[A-Za-z0-9_-]+$' AND octet_length(material_envelope) BETWEEN 65 AND 1024)
   OR (method IN ('refresh_token','auth_json') AND material_envelope ~ '^gci1\.[A-Za-z0-9_-]+$' AND octet_length(material_envelope) BETWEEN 44 AND 49152)
   OR (method='native' AND material_envelope ~ '^gcm1\.[A-Za-z0-9_-]+$' AND octet_length(material_envelope) BETWEEN 44 AND 98304)))
 OR (state IN ('unknown','completed','expired') AND material_envelope=''));
ALTER TABLE gateway_oauth_connect_intents ADD CONSTRAINT gateway_native_connect_shape CHECK (
 (client_id='native-connection-v1' AND method='native' AND connection_binding IS NOT NULL
  AND gateway_native_connection_mount_valid(native_mount) IS TRUE
  AND native_dispatch_family IS NOT NULL AND native_dispatch_family ~ '^[a-z][a-z0-9_-]{0,127}$'
  AND native_platform IS NOT NULL AND native_platform ~ '^[a-z][a-z0-9_-]{0,127}$'
  AND native_account_type IS NOT NULL AND native_account_type IN ('apikey','oauth','setup-token','upstream','bedrock','service_account')
  AND ((state='completed' AND native_publication IS NOT NULL
    AND gateway_native_connection_publication_valid(native_publication) IS TRUE
    AND native_publication->'connection'=connection_binding AND native_publication->>'dispatchFamily'=native_dispatch_family)
   OR (state<>'completed' AND native_publication IS NULL)))
 OR (client_id='app_EMoamEEZ73f0CkXaXp7hrann' AND method<>'native'
  AND native_mount IS NULL AND native_dispatch_family IS NULL AND native_platform IS NULL AND native_account_type IS NULL AND native_publication IS NULL));

-- A native physical birth is inert and owned by the existing entered journal.
-- Safe publication survives credential erasure so cleanup keeps exact receipts.
CREATE FUNCTION gateway_native_connection_account_valid(candidate accounts, previous accounts, operation text)
RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE intent gateway_oauth_connect_intents%ROWTYPE; erasing boolean; version bigint;
BEGIN
 IF operation='DELETE' THEN RETURN false; END IF;
 IF candidate.extra->'gateway_native_connection_v1' IS DISTINCT FROM 'true'::jsonb
  OR candidate.extra - ARRAY['gateway_native_connection_v1','gateway_generation_v1','gateway_profile_v1','gateway_model_v1','gateway_credential_scope_v1','gateway_connection_operation_v1'] <> '{}'::jsonb
  OR candidate.status<>'disabled' OR candidate.schedulable OR candidate.proxy_id IS NOT NULL OR candidate.parent_account_id IS NOT NULL
  OR candidate.extra->'gateway_credential_scope_v1'->'purpose' IS DISTINCT FROM '"provider-oauth-bundle-v1"'::jsonb
  OR gateway_native_scope_valid(jsonb_set(candidate.extra->'gateway_credential_scope_v1','{purpose}','"provider-authorization-v1"'),candidate.extra->>'gateway_generation_v1') IS DISTINCT FROM TRUE
  OR EXISTS(SELECT 1 FROM account_groups g WHERE g.account_id=candidate.id) THEN RETURN false; END IF;
 SELECT * INTO intent FROM gateway_oauth_connect_intents c
  WHERE c.consumer=candidate.extra->'gateway_credential_scope_v1'->>'consumer'
   AND c.owner_ref=candidate.extra->'gateway_credential_scope_v1'->>'owner'
   AND c.account_ref=candidate.extra->'gateway_credential_scope_v1'->>'account'
   AND c.generation=candidate.extra->>'gateway_generation_v1'
   AND c.operation_ref=candidate.extra->>'gateway_connection_operation_v1'
   AND c.client_id='native-connection-v1' AND NOT c.recovery_denied;
 IF NOT FOUND OR intent.native_platform<>candidate.platform OR intent.native_account_type<>candidate.type
  OR intent.connection_binding->>'profileId' IS DISTINCT FROM candidate.extra->>'gateway_profile_v1'
  OR intent.native_mount->>'model' IS DISTINCT FROM candidate.extra->>'gateway_model_v1'
  OR candidate.credentials->'connection_publication'->'connection' IS DISTINCT FROM intent.connection_binding
  OR candidate.credentials->'connection_publication'->>'dispatchFamily' IS DISTINCT FROM intent.native_dispatch_family
  OR gateway_native_connection_publication_valid(candidate.credentials->'connection_publication') IS DISTINCT FROM TRUE THEN RETURN false; END IF;
 version := gateway_native_connection_credential_version(candidate.credentials);
 IF operation='INSERT' THEN
  RETURN intent.state IN ('entered','unknown') AND intent.enrollment_mac IS NOT NULL
   AND intent.native_publication IS NULL AND candidate.deleted_at IS NULL AND version=1;
 END IF;
 IF previous.extra->'gateway_native_connection_v1' IS DISTINCT FROM 'true'::jsonb
  OR candidate.id IS DISTINCT FROM previous.id OR candidate.created_at IS DISTINCT FROM previous.created_at
  OR candidate.extra IS DISTINCT FROM previous.extra OR candidate.platform IS DISTINCT FROM previous.platform OR candidate.type IS DISTINCT FROM previous.type
  OR (previous.deleted_at IS NOT NULL AND candidate.deleted_at IS DISTINCT FROM previous.deleted_at)
  OR intent.state<>'completed' OR intent.outcome_account_id<>candidate.id
  OR candidate.credentials->'connection_publication' IS DISTINCT FROM intent.native_publication THEN RETURN false; END IF;
 IF candidate.credentials IS NOT DISTINCT FROM previous.credentials THEN
	 RETURN candidate.deleted_at IS NOT DISTINCT FROM previous.deleted_at
	  AND (to_jsonb(candidate)-'updated_at')=(to_jsonb(previous)-'updated_at');
 END IF;
 erasing := previous.deleted_at IS NULL AND candidate.deleted_at IS NOT NULL
  AND previous.credentials ? 'connection_bundle' AND candidate.credentials=previous.credentials-'connection_bundle';
	IF erasing THEN RETURN (to_jsonb(candidate)-ARRAY['credentials','deleted_at','updated_at'])=(to_jsonb(previous)-ARRAY['credentials','deleted_at','updated_at']); END IF;
 RETURN previous.deleted_at IS NULL AND candidate.deleted_at IS NULL
  AND (to_jsonb(candidate)-ARRAY['credentials','updated_at'])=(to_jsonb(previous)-ARRAY['credentials','updated_at'])
  AND version=gateway_native_connection_credential_version(previous.credentials)+1
  AND EXISTS(SELECT 1 FROM gateway_oauth_refresh_attempts f WHERE f.account_id=previous.id
   AND f.native_created_at=previous.created_at AND f.consumer=intent.consumer AND f.owner_ref=intent.owner_ref
   AND f.account_ref=intent.account_ref AND f.generation=intent.generation
   AND f.expected_version=gateway_native_connection_credential_version(previous.credentials)
   AND f.result_version=version AND f.state='completed' AND f.deadline>clock_timestamp() AND f.publication_xid=pg_current_xact_id());
EXCEPTION WHEN OTHERS THEN RETURN false;
END;
$$;

-- Foreign-key coverage is widened to physical accounts, while the existing
-- transition guard below still requires exact legacy reservation or native
-- completed binding. This does not permit arbitrary scheduler accounts.
ALTER TABLE gateway_oauth_refresh_attempts DROP CONSTRAINT gateway_oauth_refresh_attempts_account_id_fkey;
ALTER TABLE gateway_oauth_refresh_attempts ADD CONSTRAINT gateway_oauth_refresh_attempts_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id);


-- Extend the existing guards; all old transition/birth conditions remain.
CREATE OR REPLACE FUNCTION gateway_oauth_connect_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'connect evidence must be retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  -- Shared with the F1 insertion guard below: a preexisting enrollment cannot
  -- be appropriated by a connect, even at a private-operation collision.
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.consumer || chr(1) || NEW.enrollment_operation,245));
  IF EXISTS(SELECT 1 FROM gateway_oauth_identity_reservations r
   WHERE r.consumer=NEW.consumer AND r.operation_ref=NEW.enrollment_operation) THEN
   RAISE EXCEPTION 'connect enrollment operation is already owned' USING ERRCODE='23514';
  END IF;
  IF NEW.state<>'prepared' OR NEW.entered_at IS NOT NULL OR NEW.enrollment_mac IS NOT NULL OR NEW.recovery_denied OR NEW.deadline<=clock_timestamp()
   OR NEW.deadline>clock_timestamp()+interval '10 minutes' OR NEW.device_phase<>'' OR NEW.device_revision<>0 OR NEW.device_poll_after IS NOT NULL OR NEW.device_deadline IS NOT NULL THEN
   RAISE EXCEPTION 'invalid prepared connect' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (to_jsonb(NEW)-ARRAY['state','material_envelope','entered_at','outcome_operation','outcome_account_id','outcome_generation','outcome_state','enrollment_mac','recovery_denied','device_phase','device_revision','device_poll_after','device_deadline','native_publication'])
  IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['state','material_envelope','entered_at','outcome_operation','outcome_account_id','outcome_generation','outcome_state','enrollment_mac','recovery_denied','device_phase','device_revision','device_poll_after','device_deadline','native_publication']) THEN
  RAISE EXCEPTION 'connect intent is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.native_publication IS DISTINCT FROM OLD.native_publication AND NOT (
  OLD.client_id='native-connection-v1' AND OLD.native_publication IS NULL AND NEW.state='completed'
  AND gateway_native_connection_publication_valid(NEW.native_publication) IS TRUE
  AND NEW.native_publication->'connection'=NEW.connection_binding
  AND NEW.native_publication->>'dispatchFamily'=NEW.native_dispatch_family) THEN
  RAISE EXCEPTION 'native credential publication is sealed' USING ERRCODE='23514';
 END IF;
 -- Device request and poll claims are bounded extensions of prepared state.
 -- They cannot alter authority or enter custody. A spent/terminal intent still
 -- follows the existing one-way entry and exact F1 commitment transitions.
 IF NEW.state='prepared' AND OLD.state='prepared' AND OLD.method='device_code' THEN
  IF (to_jsonb(NEW)-ARRAY['material_envelope','device_phase','device_revision','device_poll_after','device_deadline','native_publication'])
   IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['material_envelope','device_phase','device_revision','device_poll_after','device_deadline','native_publication'])
   OR NEW.deadline<=clock_timestamp() THEN
   RAISE EXCEPTION 'device prepared transition denied' USING ERRCODE='23514';
  END IF;
  IF OLD.device_phase='' AND NEW.device_phase='starting' THEN
   IF NEW.device_revision<>OLD.device_revision+1 OR NEW.material_envelope IS DISTINCT FROM OLD.material_envelope
    OR NEW.device_poll_after IS DISTINCT FROM OLD.device_poll_after OR NEW.device_deadline IS DISTINCT FROM OLD.device_deadline THEN
    RAISE EXCEPTION 'device begin claim denied' USING ERRCODE='23514';
   END IF;
  ELSIF OLD.device_phase='pending' AND NEW.device_phase='polling' THEN
   IF NEW.device_revision<>OLD.device_revision+1 OR OLD.device_poll_after>clock_timestamp()
    OR NEW.device_deadline IS DISTINCT FROM OLD.device_deadline OR NEW.device_poll_after IS DISTINCT FROM OLD.device_poll_after OR NEW.material_envelope IS DISTINCT FROM OLD.material_envelope THEN
    RAISE EXCEPTION 'device poll claim denied' USING ERRCODE='23514';
   END IF;
  ELSIF OLD.device_phase IN ('starting','polling') AND NEW.device_phase='pending' THEN
   IF NEW.device_revision<>OLD.device_revision OR NEW.device_poll_after IS NULL
    OR NEW.device_poll_after<=clock_timestamp() OR NEW.device_poll_after>clock_timestamp()+interval '301 seconds'
    OR NEW.device_deadline IS NULL OR NEW.device_deadline<=clock_timestamp() OR NEW.device_deadline>NEW.deadline
    OR (OLD.device_phase='polling' AND (NEW.material_envelope IS DISTINCT FROM OLD.material_envelope OR NEW.device_deadline IS DISTINCT FROM OLD.device_deadline)) THEN
    RAISE EXCEPTION 'device pending result denied' USING ERRCODE='23514';
   END IF;
  ELSE RAISE EXCEPTION 'device replay denied' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.device_phase IS DISTINCT FROM OLD.device_phase OR NEW.device_revision IS DISTINCT FROM OLD.device_revision
  OR NEW.device_poll_after IS DISTINCT FROM OLD.device_poll_after OR NEW.device_deadline IS DISTINCT FROM OLD.device_deadline THEN
  RAISE EXCEPTION 'device claim is immutable outside prepared transition' USING ERRCODE='23514';
 END IF;
 IF OLD.recovery_denied OR (OLD.enrollment_mac IS NOT NULL AND NEW.enrollment_mac IS DISTINCT FROM OLD.enrollment_mac) THEN
  RAISE EXCEPTION 'connect custody binding is sealed' USING ERRCODE='23514';
 END IF;
 IF NEW.enrollment_mac IS DISTINCT FROM OLD.enrollment_mac THEN
  IF OLD.state NOT IN ('entered','unknown') OR NEW.state IS DISTINCT FROM OLD.state
   OR NEW.material_envelope IS DISTINCT FROM OLD.material_envelope OR NEW.entered_at IS DISTINCT FROM OLD.entered_at
   OR NEW.recovery_denied IS DISTINCT FROM OLD.recovery_denied
   OR EXISTS(SELECT 1 FROM gateway_oauth_identity_reservations r WHERE r.consumer=NEW.consumer AND r.operation_ref=NEW.enrollment_operation) THEN
   RAISE EXCEPTION 'connect binding denied' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.state='completed' AND NOT (
  NEW.client_id='native-connection-v1' AND EXISTS(SELECT 1 FROM accounts a
   WHERE a.id=NEW.outcome_account_id AND a.status='disabled' AND NOT a.schedulable
    AND a.deleted_at IS NULL AND a.proxy_id IS NULL AND a.parent_account_id IS NULL
    AND a.platform=NEW.native_platform AND a.type=NEW.native_account_type
    AND a.extra->'gateway_native_connection_v1'='true'::jsonb
    AND a.extra->>'gateway_generation_v1'=NEW.generation
    AND a.extra->>'gateway_profile_v1'=NEW.connection_binding->>'profileId'
    AND a.extra->>'gateway_model_v1'=NEW.native_mount->>'model'
    AND a.extra->>'gateway_connection_operation_v1'=NEW.operation_ref
    AND a.extra->'gateway_credential_scope_v1'=jsonb_build_object('consumer',NEW.consumer,'owner',NEW.owner_ref,'account',NEW.account_ref,'generation',NEW.generation,'purpose',NEW.purpose)
    AND a.credentials->'connection_publication'=NEW.native_publication
    AND gateway_native_connection_credential_version(a.credentials)=1
    AND NOT EXISTS(SELECT 1 FROM account_groups g WHERE g.account_id=a.id))) AND NOT EXISTS(
  SELECT 1 FROM gateway_oauth_identity_reservations r JOIN accounts a ON a.id=r.account_id
  WHERE r.consumer=NEW.consumer AND r.owner_ref=NEW.owner_ref AND r.account_ref=NEW.account_ref
   AND r.generation=NEW.generation AND r.operation_ref=NEW.enrollment_operation AND r.intent_mac=NEW.enrollment_mac
   AND r.account_id=NEW.outcome_account_id
   AND a.extra->>'gateway_generation_v1'=NEW.generation
   AND a.extra->>'gateway_profile_v1'='openai-oidc-oauth-staging-v1'
   AND a.extra->'gateway_credential_scope_v1'->>'consumer'=NEW.consumer
   AND a.extra->'gateway_credential_scope_v1'->>'owner'=NEW.owner_ref
   AND a.extra->'gateway_credential_scope_v1'->>'account'=NEW.account_ref
   AND a.extra->'gateway_credential_scope_v1'->>'purpose'=NEW.purpose
   AND a.status='disabled' AND NOT a.schedulable
   AND NOT EXISTS(SELECT 1 FROM account_groups g WHERE g.account_id=a.id)) THEN
  RAISE EXCEPTION 'connect completion requires exact custody birth' USING ERRCODE='23514';
 END IF;
 IF OLD.state IN ('completed','expired') THEN
  RAISE EXCEPTION 'connect terminal result is immutable' USING ERRCODE='23514';
 ELSIF OLD.state='prepared' THEN
  IF NEW.state='entered' THEN
   IF NEW.deadline<=clock_timestamp() OR NEW.entered_at IS NULL OR NEW.material_envelope IS DISTINCT FROM OLD.material_envelope THEN
    RAISE EXCEPTION 'connect entry denied' USING ERRCODE='23514';
   END IF;
  ELSIF NEW.state='expired' THEN
   IF (NEW.deadline>clock_timestamp() AND (NEW.method<>'device_code' OR NEW.device_deadline IS NULL OR NEW.device_deadline>clock_timestamp())) OR NEW.entered_at IS NOT NULL OR NEW.material_envelope<>'' THEN
    RAISE EXCEPTION 'connect expiry denied' USING ERRCODE='23514';
   END IF;
  ELSE RAISE EXCEPTION 'connect transition denied' USING ERRCODE='23514';
  END IF;
 ELSIF OLD.state IN ('entered','unknown') THEN
  IF NEW.state NOT IN ('unknown','completed') OR NEW.entered_at IS DISTINCT FROM OLD.entered_at OR NEW.material_envelope<>'' THEN
   RAISE EXCEPTION 'spent connect cannot rearm' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION public.gateway_oauth_refresh_attempt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'refresh attempt must be retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (to_jsonb(NEW)-ARRAY['fence','state','deadline','result_version','publication_xid']) IS DISTINCT FROM
     (to_jsonb(OLD)-ARRAY['fence','state','deadline','result_version','publication_xid'])
   OR OLD.state IN ('completed','unknown') THEN
   RAISE EXCEPTION 'refresh identity and terminal facts are immutable' USING ERRCODE='23514';
  END IF;
  IF NOT (
   (OLD.state='prepared' AND NEW.state='prepared' AND OLD.deadline<=clock_timestamp()
    AND NEW.fence=OLD.fence+1 AND NEW.deadline>clock_timestamp() AND NEW.deadline<=clock_timestamp()+interval '15 seconds')
   OR (NEW.fence=OLD.fence AND NEW.deadline=OLD.deadline AND (
    (OLD.state='prepared' AND NEW.state='entered' AND OLD.deadline>clock_timestamp())
    OR (NEW.state='unknown' AND OLD.state IN ('prepared','entered'))
    OR (OLD.state='entered' AND NEW.state='completed' AND OLD.deadline>clock_timestamp()
     AND NEW.publication_xid=pg_current_xact_id())))) THEN
   RAISE EXCEPTION 'invalid refresh fence or transition' USING ERRCODE='23514';
  END IF;
 ELSE
  IF NEW.state<>'prepared' OR NEW.fence IS DISTINCT FROM (SELECT COALESCE(max(fence),0)+1 FROM gateway_oauth_refresh_attempts WHERE account_id=NEW.account_id) OR NEW.deadline<=clock_timestamp()
   OR NEW.deadline>clock_timestamp()+interval '15 seconds' THEN
   RAISE EXCEPTION 'invalid refresh preparation' USING ERRCODE='23514';
  END IF;
 END IF;
 -- UNKNOWN records retain ambiguity even after erasure. Every transition that
 -- can authorize entry/publication must match this exact inert engine snapshot.
 IF NEW.state<>'unknown' AND NOT EXISTS (
  SELECT 1 FROM accounts a JOIN gateway_oauth_connect_intents c ON c.outcome_account_id=a.id
  WHERE a.id=NEW.account_id AND a.created_at=NEW.native_created_at
   AND c.consumer=NEW.consumer AND c.owner_ref=NEW.owner_ref AND c.account_ref=NEW.account_ref AND c.generation=NEW.generation
   AND c.client_id='native-connection-v1' AND c.state='completed' AND NOT c.recovery_denied
   AND a.extra->'gateway_native_connection_v1'='true'::jsonb AND a.extra->>'gateway_connection_operation_v1'=c.operation_ref
   AND a.extra->'gateway_credential_scope_v1'=jsonb_build_object('consumer',c.consumer,'owner',c.owner_ref,'account',c.account_ref,'generation',c.generation,'purpose',c.purpose)
   AND a.credentials->'connection_publication'=c.native_publication
   AND a.credentials->'connection_publication'->'credential'->>'refreshPolicy'='native'
   AND a.status='disabled' AND NOT a.schedulable AND a.proxy_id IS NULL AND a.parent_account_id IS NULL AND a.deleted_at IS NULL
   AND NOT EXISTS(SELECT 1 FROM account_groups g WHERE g.account_id=a.id)
   AND gateway_native_connection_credential_version(a.credentials)=NEW.expected_version) AND NOT EXISTS (
  SELECT 1 FROM accounts a JOIN gateway_oauth_identity_reservations r ON r.account_id=a.id
  WHERE a.id=NEW.account_id AND a.created_at=NEW.native_created_at
   AND r.consumer=NEW.consumer AND r.owner_ref=NEW.owner_ref AND r.account_ref=NEW.account_ref AND r.generation=NEW.generation
   AND a.extra->>'gateway_generation_v1'=r.generation
   AND a.extra->>'gateway_profile_v1'='openai-oidc-oauth-staging-v1'
   AND a.extra->'gateway_credential_scope_v1'=jsonb_build_object('consumer',r.consumer,'owner',r.owner_ref,'account',r.account_ref,'generation',r.generation,'purpose','provider-oauth-bundle-v1')
   AND a.platform='openai' AND a.type='oauth' AND a.status='disabled' AND NOT a.schedulable
   AND a.proxy_id IS NULL AND a.parent_account_id IS NULL AND a.deleted_at IS NULL
   AND NOT EXISTS(SELECT 1 FROM account_groups g WHERE g.account_id=a.id)
   AND public.gateway_oauth_credential_version(a.credentials)=NEW.expected_version) THEN
  RAISE EXCEPTION 'refresh requires exact birth and credential version' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION public.gateway_oauth_refresh_publication_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- UPDATE-time checks cannot authorize a transaction held beyond its entered
 -- deadline. Recheck at the deferred publication boundary before commit.
 IF NEW.state='completed' AND NEW.deadline<=clock_timestamp() THEN
  RAISE EXCEPTION 'refresh publication deadline expired' USING ERRCODE='23514';
 END IF;
 IF NEW.state='completed' AND NOT EXISTS(SELECT 1 FROM accounts a
  WHERE a.id=NEW.account_id AND a.created_at=NEW.native_created_at
   AND (public.gateway_oauth_credential_version(a.credentials)>=NEW.result_version OR gateway_native_connection_credential_version(a.credentials)>=NEW.result_version)) THEN
  RAISE EXCEPTION 'refresh completion requires atomic publication' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION public.gateway_native_account_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE marked boolean; old_marked boolean; erasing boolean := false;
BEGIN
 IF (TG_OP<>'DELETE' AND NEW.extra->'gateway_native_connection_v1'='true'::jsonb)
  OR (TG_OP<>'INSERT' AND OLD.extra->'gateway_native_connection_v1'='true'::jsonb) THEN
  IF gateway_native_connection_account_valid(NEW,OLD,TG_OP) IS DISTINCT FROM TRUE THEN
   RAISE EXCEPTION 'invalid protected native connection birth or custody' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;

 -- The original 242 API-key branch below is retained verbatim; only OAuth changes.
 IF TG_OP <> 'DELETE' AND (NEW.extra ->> 'gateway_profile_v1' = 'openai-oidc-oauth-staging-v1'
  OR (TG_OP = 'UPDATE' AND OLD.extra ->> 'gateway_profile_v1' = 'openai-oidc-oauth-staging-v1')) THEN
  IF TG_OP = 'UPDATE' AND (
   OLD.extra ->> 'gateway_profile_v1' IS DISTINCT FROM 'openai-oidc-oauth-staging-v1'
   OR NEW.id IS DISTINCT FROM OLD.id OR NEW.created_at IS DISTINCT FROM OLD.created_at
   OR NEW.extra IS DISTINCT FROM OLD.extra OR NEW.platform IS DISTINCT FROM OLD.platform
   OR NEW.type IS DISTINCT FROM OLD.type
   OR (OLD.deleted_at IS NOT NULL AND NEW.deleted_at IS DISTINCT FROM OLD.deleted_at)
   OR (NEW.credentials IS DISTINCT FROM OLD.credentials AND NOT (
    NEW.credentials = '{}'::jsonb AND OLD.credentials ? 'oauth_bundle' AND NEW.deleted_at IS NOT NULL)
    AND NOT (
     OLD.deleted_at IS NULL AND NEW.deleted_at IS NULL
     AND (to_jsonb(NEW)-ARRAY['credentials','updated_at'])=(to_jsonb(OLD)-ARRAY['credentials','updated_at'])
     AND public.gateway_oauth_credential_version(NEW.credentials)=public.gateway_oauth_credential_version(OLD.credentials)+1
     AND EXISTS(SELECT 1 FROM gateway_oauth_refresh_attempts f
      WHERE f.account_id=OLD.id AND f.native_created_at=OLD.created_at
       AND f.consumer=OLD.extra->'gateway_credential_scope_v1'->>'consumer'
       AND f.owner_ref=OLD.extra->'gateway_credential_scope_v1'->>'owner'
       AND f.account_ref=OLD.extra->'gateway_credential_scope_v1'->>'account'
       AND f.generation=OLD.extra->>'gateway_generation_v1'
       AND f.expected_version=public.gateway_oauth_credential_version(OLD.credentials)
       AND f.result_version=public.gateway_oauth_credential_version(NEW.credentials)
       AND f.state='completed' AND f.deadline>clock_timestamp()
       AND f.publication_xid=pg_current_xact_id())))) THEN
   RAISE EXCEPTION 'gateway oauth birth and custody are immutable' USING ERRCODE = '23514';
  END IF;
  IF NEW.platform <> 'openai' OR NEW.type <> 'oauth' OR NEW.status <> 'disabled'
   OR NEW.schedulable OR NEW.proxy_id IS NOT NULL OR NEW.parent_account_id IS NOT NULL
   OR jsonb_typeof(NEW.extra) IS DISTINCT FROM 'object'
   OR (NEW.extra - ARRAY['gateway_generation_v1','gateway_profile_v1','gateway_credential_scope_v1']) <> '{}'::jsonb
   OR jsonb_typeof(NEW.extra -> 'gateway_generation_v1') IS DISTINCT FROM 'string'
   OR COALESCE(NEW.extra ->> 'gateway_generation_v1','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR NEW.extra ->> 'gateway_generation_v1' = '00000000-0000-0000-0000-000000000000'
   OR NEW.extra -> 'gateway_credential_scope_v1' -> 'purpose' IS DISTINCT FROM '"provider-oauth-bundle-v1"'::jsonb
   OR public.gateway_native_scope_valid(
    jsonb_set(NEW.extra -> 'gateway_credential_scope_v1','{purpose}','"provider-authorization-v1"'),
    NEW.extra ->> 'gateway_generation_v1') IS DISTINCT FROM TRUE
   OR jsonb_typeof(NEW.credentials) IS DISTINCT FROM 'object'
   OR (NEW.credentials ? 'oauth_bundle' AND public.gateway_oauth_credential_version(NEW.credentials)=0)
   OR (TG_OP='INSERT' AND public.gateway_oauth_credential_version(NEW.credentials)<>1)
   OR (NOT (NEW.credentials ? 'oauth_bundle') AND NEW.credentials <> '{}'::jsonb)
   OR (NOT (NEW.credentials ? 'oauth_bundle') AND (TG_OP = 'INSERT' OR NEW.deleted_at IS NULL))
   OR (TG_OP = 'INSERT' AND NEW.deleted_at IS NOT NULL)
   OR EXISTS(SELECT 1 FROM account_groups g WHERE g.account_id=NEW.id)
   OR NOT EXISTS(SELECT 1 FROM gateway_oauth_identity_reservations r
    WHERE r.generation=NEW.extra ->> 'gateway_generation_v1'
    AND r.consumer=NEW.extra -> 'gateway_credential_scope_v1' ->> 'consumer'
    AND r.owner_ref=NEW.extra -> 'gateway_credential_scope_v1' ->> 'owner'
    AND r.account_ref=NEW.extra -> 'gateway_credential_scope_v1' ->> 'account'
    AND (r.account_id IS NULL OR r.account_id=NEW.id)) THEN
   RAISE EXCEPTION 'invalid protected oauth staging candidate' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
 END IF;
 IF TG_OP = 'DELETE' THEN
  IF COALESCE(OLD.extra, '{}'::jsonb) ?| ARRAY['gateway_generation_v1','gateway_profile_v1','gateway_credential_scope_v1'] THEN
   RAISE EXCEPTION 'gateway generation tombstone must be retained' USING ERRCODE = '23514';
  END IF;
  RETURN OLD;
 END IF;
 marked := COALESCE(NEW.extra, '{}'::jsonb) ?| ARRAY['gateway_generation_v1','gateway_profile_v1','gateway_credential_scope_v1'];
 IF TG_OP = 'UPDATE' THEN
  old_marked := COALESCE(OLD.extra, '{}'::jsonb) ?| ARRAY['gateway_generation_v1','gateway_profile_v1','gateway_credential_scope_v1'];
  IF old_marked THEN
   IF NEW.id IS DISTINCT FROM OLD.id OR NEW.created_at IS DISTINCT FROM OLD.created_at
    OR NEW.extra IS DISTINCT FROM OLD.extra
    OR NEW.platform IS DISTINCT FROM OLD.platform OR NEW.type IS DISTINCT FROM OLD.type
    OR (OLD.deleted_at IS NOT NULL AND NEW.deleted_at IS DISTINCT FROM OLD.deleted_at) THEN
    RAISE EXCEPTION 'gateway native generation and custody scope are immutable' USING ERRCODE = '23514';
   END IF;
   IF NEW.credentials IS DISTINCT FROM OLD.credentials THEN
    erasing := OLD.status = 'disabled' AND NEW.status = 'disabled'
     AND NEW.deleted_at IS NOT NULL AND NEW.credentials = OLD.credentials - 'api_key'
     AND OLD.credentials ? 'api_key';
    IF erasing IS DISTINCT FROM TRUE THEN
     RAISE EXCEPTION 'gateway credential replacement is forbidden' USING ERRCODE = '23514';
    END IF;
   END IF;
  ELSIF marked THEN
   RAISE EXCEPTION 'gateway native identity requires a new candidate' USING ERRCODE = '23514';
  END IF;
 END IF;
 IF marked THEN
  IF jsonb_typeof(NEW.extra) IS DISTINCT FROM 'object'
   OR jsonb_typeof(NEW.extra -> 'gateway_generation_v1') IS DISTINCT FROM 'string'
   OR COALESCE(NEW.extra ->> 'gateway_generation_v1', '') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR NEW.extra ->> 'gateway_generation_v1' = '00000000-0000-0000-0000-000000000000'
   OR public.gateway_native_scope_valid(NEW.extra -> 'gateway_credential_scope_v1', NEW.extra ->> 'gateway_generation_v1') IS DISTINCT FROM TRUE
   OR (
    (NEW.extra ->> 'gateway_profile_v1' = 'openai-responses-apikey-v1'
     AND NEW.extra ->> 'openai_responses_mode' = 'force_responses'
     AND NEW.extra -> 'openai_passthrough' = 'true'::jsonb)
    OR (NEW.extra ->> 'gateway_profile_v1' = 'openrouter-openai-responses-apikey-v1'
     AND NEW.extra ->> 'openai_responses_mode' = 'force_responses'
     AND NEW.extra -> 'openai_passthrough' = 'true'::jsonb
     AND NEW.credentials ->> 'base_url' = 'https://openrouter.ai/api/v1'
     AND (NEW.extra ->> 'gateway_model_v1') COLLATE "C" ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'
     AND strpos(NEW.extra ->> 'gateway_model_v1', '/') > 0
     AND strpos(NEW.extra ->> 'gateway_model_v1', '://') = 0)
    OR (NEW.extra ->> 'gateway_profile_v1' = 'kimi-code-global-responses-apikey-v1'
     AND NEW.extra ->> 'openai_responses_mode' = 'force_responses'
     AND NEW.extra -> 'openai_passthrough' = 'true'::jsonb
     AND NEW.credentials ->> 'base_url' = 'https://api.kimi.ai/coding/v1'
     AND NEW.extra ->> 'gateway_model_v1' = 'k3-256k')
    OR (NEW.extra ->> 'gateway_profile_v1' = 'glm-coding-global-responses-apikey-v1'
     AND NEW.extra ->> 'openai_responses_mode' = 'force_responses'
     AND NEW.extra -> 'openai_passthrough' = 'true'::jsonb
     AND NEW.credentials ->> 'base_url' = 'https://api.z.ai/api/v1'
     AND NEW.extra ->> 'gateway_model_v1' = 'glm-5.3-flash')
    OR (NEW.extra ->> 'gateway_profile_v1' = 'minimax-mplan-global-responses-apikey-v1'
     AND NEW.extra ->> 'openai_responses_mode' = 'force_responses'
     AND NEW.extra -> 'openai_passthrough' = 'true'::jsonb
     AND NEW.credentials ->> 'base_url' = 'https://api.minimax.io/v1'
     AND NEW.extra ->> 'gateway_model_v1' = 'MiniMax-M3.1-Flash')
    OR (NEW.extra ->> 'gateway_profile_v1' = 'nvidia-build-chat-apikey-v1'
     AND NEW.extra ->> 'openai_responses_mode' = 'force_chat_completions'
     AND NEW.extra -> 'openai_passthrough' = 'false'::jsonb
     AND NEW.credentials ->> 'base_url' = 'https://integrate.api.nvidia.com/v1'
     AND NEW.extra ->> 'gateway_model_v1' = 'nvidia/nemotron-3.5-super-vl-120b-a12b')
    OR (NEW.extra ->> 'gateway_profile_v1' = 'anthropic-messages-apikey-v1'
     AND NEW.extra ->> 'openai_responses_mode' = 'force_responses'
     AND NEW.extra -> 'openai_passthrough' = 'true'::jsonb
     AND NEW.credentials ->> 'base_url' = 'https://api.anthropic.com/v1'
     AND NEW.extra ->> 'gateway_model_v1' = 'claude-sonnet-4-6')
    OR (NEW.extra ->> 'gateway_profile_v1' = 'gemini-openai-chat-apikey-v1'
     AND NEW.extra ->> 'openai_responses_mode' = 'force_responses'
     AND NEW.extra -> 'openai_passthrough' = 'true'::jsonb
     AND NEW.credentials ->> 'base_url' = 'https://generativelanguage.googleapis.com/v1beta/openai'
     AND NEW.extra ->> 'gateway_model_v1' = 'gemini-3.1-pro-preview')
    OR (NEW.extra ->> 'gateway_profile_v1' = 'mimo-token-plan-responses-chat-bridge-v1'
     AND NEW.extra ->> 'openai_responses_mode' = 'force_chat_completions'
     AND NEW.extra -> 'openai_passthrough' = 'false'::jsonb)
   ) IS DISTINCT FROM TRUE
   OR NEW.extra -> 'native_api_key_cancel_on_disconnect' IS DISTINCT FROM 'true'::jsonb
   OR NEW.extra -> 'openai_preserve_compatible_reasoning' IS DISTINCT FROM 'true'::jsonb
   OR jsonb_typeof(NEW.extra -> 'gateway_model_v1') IS DISTINCT FROM 'string'
   OR COALESCE(length(btrim(NEW.extra ->> 'gateway_model_v1')), 0) = 0
   OR (NEW.extra - ARRAY['gateway_generation_v1','gateway_profile_v1','gateway_model_v1','gateway_credential_scope_v1','openai_responses_mode','openai_passthrough','native_api_key_cancel_on_disconnect','openai_preserve_compatible_reasoning']) <> '{}'::jsonb
   OR NEW.platform <> 'openai' OR NEW.type <> 'apikey' OR NEW.schedulable
   OR NEW.parent_account_id IS NOT NULL OR NEW.proxy_id IS NOT NULL
   OR jsonb_typeof(NEW.credentials) IS DISTINCT FROM 'object'
   OR (NEW.credentials ? 'api_key' AND public.gateway_native_envelope_valid(NEW.credentials -> 'api_key') IS DISTINCT FROM TRUE)
   OR (NOT (NEW.credentials ? 'api_key') AND (TG_OP = 'INSERT' OR NEW.status <> 'disabled' OR NEW.deleted_at IS NULL))
   OR jsonb_typeof(NEW.credentials -> 'base_url') IS DISTINCT FROM 'string'
   OR COALESCE(length(btrim(NEW.credentials ->> 'base_url')), 0) = 0
   OR (NEW.credentials - ARRAY['api_key','base_url']) <> '{}'::jsonb THEN
   RAISE EXCEPTION 'invalid encrypted gateway native candidate' USING ERRCODE = '23514';
  END IF;
  IF NEW.deleted_at IS NOT NULL AND NEW.status <> 'disabled' THEN
   RAISE EXCEPTION 'gateway tombstone must stay disabled' USING ERRCODE = '23514';
  END IF;
  IF TG_OP = 'INSERT' AND (NEW.status <> 'disabled' OR NEW.deleted_at IS NOT NULL) THEN
   RAISE EXCEPTION 'gateway native creation must be inert' USING ERRCODE = '23514';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
