-- A trusted control-plane scope expands at query time, without pagination caps.
-- Plain tenant scopes preserve the existing single-tenant contract.
CREATE OR REPLACE FUNCTION analytics_scope_tenants(scope text) RETURNS SETOF text
LANGUAGE plpgsql STABLE AS $$
BEGIN
 IF left(scope,4) = 'org:' THEN
  RETURN QUERY SELECT id::text FROM tenants WHERE organization_id=substring(scope FROM 5);
 ELSE
  RETURN NEXT scope;
 END IF;
END;
$$;
