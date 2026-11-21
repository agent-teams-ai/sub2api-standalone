-- Extend the original closed observation vocabulary to the complete native
-- account-info policy. Preserve every stored source, principal and revision;
-- migration 260 and its published checksum remain unchanged.
DO $$
DECLARE source_check text;
BEGIN
 SELECT conname INTO STRICT source_check FROM pg_constraint
 WHERE conrelid='gateway_account_info_snapshots'::regclass AND contype='c'
  AND conkey=ARRAY[(SELECT attnum FROM pg_attribute
   WHERE attrelid='gateway_account_info_snapshots'::regclass AND attname='sources')]::smallint[];
 EXECUTE format('ALTER TABLE gateway_account_info_snapshots DROP CONSTRAINT %I',source_check);
END;
$$;

ALTER TABLE gateway_account_info_snapshots ADD CONSTRAINT gateway_account_info_sources_closed CHECK (
 jsonb_typeof(sources)='object' AND octet_length(sources::text)<=32768
 AND (sources - ARRAY['verified_claims','account_check','plan_quota','credential',
  'training_settings','subscription_status','user_declaration'])='{}'::jsonb);
