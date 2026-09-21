-- Remove the permanent lease sentinel before rolling back 0038. Keep the
-- legacy schedule disabled here: 0038 down subsequently restores enabled and
-- all schedule fields from sync_target_configs in one guarded step.
UPDATE directory_sync_configs
SET enabled=0,
    lease_owner=NULL,
    lease_until=NULL
WHERE id=1
  AND lease_owner='enterprise-sync-v2-fence';
