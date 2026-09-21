-- Non-destructive rollback: preserve application IDs, ACLs, favourites and audits.
-- Hide the new renderers before rolling back the frontend/API code.
UPDATE applications SET enabled=0
WHERE (slug='ldap-password' AND renderer_key='ldap-password')
 OR (slug='oa-phone' AND renderer_key='oa-phone')
 OR (slug='tpm-account' AND renderer_key='tpm-account');
