-- Mixed-version rollout fence (2026-09-20).
--
-- Migration 0038 moves scheduling to sync_target_configs, but an older
-- scheduler binary still polls directory_sync_configs. Disable that row and
-- overwrite any in-flight legacy lease with a permanent sentinel. An old
-- scheduler that already held the lease will update zero rows on RenewLease
-- and must stop with ErrLeaseLost; new schedulers never use this table.
UPDATE directory_sync_configs
SET enabled=0,
    lease_owner='enterprise-sync-v2-fence',
    lease_until='9999-12-31 23:59:59.999'
WHERE id=1;
