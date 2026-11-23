-- Reuse the bounded native observation store for ordinary administrative
-- accounts without inventing a protected consumer, owner, or generation.
ALTER TABLE gateway_account_info_snapshots ADD COLUMN subject_kind text NOT NULL
 DEFAULT 'protected' CHECK (subject_kind IN ('protected', 'admin'));

-- This is an internal database revision, not an imported token version or a
-- fingerprint of a secret. PostgreSQL's constant DEFAULT needs no fact backfill.
ALTER TABLE accounts ADD COLUMN account_info_credential_revision bigint NOT NULL
 DEFAULT 1 CHECK (account_info_credential_revision BETWEEN 1 AND 9007199254740991);

CREATE FUNCTION account_info_advance_credential_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Protected rows already have an engine-owned credential version. Their
 -- immutable-birth guards compare the remaining row, including new columns;
 -- preserve that contract for both current and rollback binaries.
 IF COALESCE(OLD.extra ?| ARRAY['gateway_generation_v1','gateway_profile_v1','gateway_credential_scope_v1',
     'gateway_claude_generation_v1','gateway_claude_profile_v1','gateway_claude_scope_v1',
     'gateway_grok_generation_v1','gateway_grok_profile_v1','gateway_grok_scope_v1'],false)
 OR COALESCE(NEW.extra ?| ARRAY['gateway_generation_v1','gateway_profile_v1','gateway_credential_scope_v1',
     'gateway_claude_generation_v1','gateway_claude_profile_v1','gateway_claude_scope_v1',
     'gateway_grok_generation_v1','gateway_grok_profile_v1','gateway_grok_scope_v1'],false) THEN
  NEW.account_info_credential_revision := OLD.account_info_credential_revision;
  RETURN NEW;
 END IF;
 IF NEW.credentials IS DISTINCT FROM OLD.credentials THEN
  IF OLD.account_info_credential_revision >= 9007199254740991 THEN
   RAISE EXCEPTION 'account metadata credential revision exhausted';
  END IF;
  NEW.account_info_credential_revision := OLD.account_info_credential_revision + 1;
 ELSE
  NEW.account_info_credential_revision := OLD.account_info_credential_revision;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER account_info_advance_credential_revision BEFORE UPDATE ON accounts
 FOR EACH ROW EXECUTE FUNCTION account_info_advance_credential_revision();
