-- One immutable selection extends existing provider journals. No new lifecycle
-- store, plaintext input, or provider credential is introduced.
CREATE FUNCTION gateway_native_connection_binding_valid(v jsonb)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT jsonb_typeof(v)='object'
  AND v ?& ARRAY['profileId','methodId','methodRevision','configurationGeneration']
  AND v - ARRAY['profileId','methodId','methodRevision','configurationGeneration']='{}'::jsonb
  AND jsonb_typeof(v->'profileId')='string'
  AND jsonb_typeof(v->'methodId')='string'
  AND jsonb_typeof(v->'configurationGeneration')='string'
  AND (v->>'profileId') ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$'
  AND (v->>'methodId') ~ '^[a-z][a-z0-9_-]{0,127}$'
  AND (v->>'configurationGeneration') ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$'
  AND jsonb_typeof(v->'methodRevision')='number'
  AND (v->>'methodRevision') ~ '^[1-9][0-9]{0,6}$'
  AND (v->>'methodRevision')::numeric <= 1000000;
$$;

ALTER TABLE gateway_oauth_connect_intents
 ADD COLUMN connection_binding jsonb
 CHECK (connection_binding IS NULL OR gateway_native_connection_binding_valid(connection_binding));
ALTER TABLE gateway_claude_connect_intents
 ADD COLUMN connection_binding jsonb
 CHECK (connection_binding IS NULL OR gateway_native_connection_binding_valid(connection_binding));
ALTER TABLE gateway_grok_connect_intents
 ADD COLUMN connection_binding jsonb
 CHECK (connection_binding IS NULL OR gateway_native_connection_binding_valid(connection_binding));

-- Existing transition guards compare all columns except their explicit mutable
-- state/effect fields, so connection_binding is immutable from first insertion.
-- Old rows remain SQL NULL and keep their original envelope/AAD decoders.
