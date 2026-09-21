# Enterprise deployment and operations

> Status date: 2026-09-20. The Django control plane is retired; enterprise
> identity, Admin RBAC, resource ACL, access groups, directory synchronization,
> and access diagnosis are implemented in Go.

## Administrator identity

Enterprise-console access is granted by either:

- `users.is_staff = 1`: break-glass super administrator. This identity receives
  every admin permission and `resource.access.bypass`.
- Admin RBAC: a non-staff active user with one or more enabled, non-expired
  global role assignments. Effective permissions are the union of all roles.

`ENTERPRISE_RBAC_ENABLED=false` keeps the legacy staff-only guard active.
`ENTERPRISE_RBAC_ENABLED=true` enables permission-code guards and the Admin Me
identity contract (`GET /api/v2/admin/me`). Frontend navigation is only a
presentation filter; every API independently checks its required permission.

System roles are seeded by migration and cannot be edited or deleted. The
platform prevents removal of the final active break-glass administrator or
Platform Owner.

## Admin RBAC

The RBAC source of truth is:

- `admin_roles`
- `admin_permissions`
- `admin_role_permissions`
- `admin_role_assignments`

Business code checks permission codes, never role names. P0 assignments use
`scope_type=global`; department-scoped administrators are reserved for a later
phase.

## Resource ACL

Application access modes remain:

- `all`
- `assigned`
- `admin_only`

Admin RBAC and resource ACL are intentionally separate. A normal administrator
does not receive `admin_only` resource access unless a role explicitly grants
`resource.access.bypass`.

The unified access decision supports direct users, direct departments, local
access groups, and Feishu user groups. It returns a stable reason code and
matched access path. Access diagnosis (`POST /api/v2/admin/access/diagnose`)
uses the same resolver semantics as runtime authorization.

Catalog pages, workspace bootstrap, resolve, mention, favorites, run/schedule
admission, and worker pre-submit SQL include the same direct and group grants.
Disabled applications and inactive users fail closed.

## Access groups

`access_groups` is the shared group abstraction:

- `source_type=feishu`: metadata and membership are read-only mirrors from
  Feishu; administrators may inspect or disable the group but cannot edit its
  members.
- `source_type=local`: administrators manage direct directory users and
  departments, including `include_children` inheritance through the directory
  closure table.

Nested groups and DENY grants are not supported. Applications bind groups
through `application_group_grants`. A group referenced by an application
cannot be deleted; disable it or remove all resource grants first.

## Feishu user-group synchronization

Directory synchronization uses application identity (`tenant_access_token`) and
publishes one consistent snapshot:

1. fetch departments, employees, and user-department relationships;
2. fetch normal and dynamic Feishu user groups plus user members;
3. write all entities to stage tables;
4. validate references and suspicious shrink thresholds;
5. publish organization, group mirror, membership mirror, and department
   closure in one MySQL transaction;
6. increment `directory_version` and mark the sync run successful.

Runtime ACL never calls Feishu. It only reads the local
`access_group_external_members` mirror. Missing Feishu groups are retained with
`enabled=0` and `sync_status=missing` so resource references and audit history
remain diagnosable.

Required Feishu APIs include directory department/employee reads and Contacts
v3 user-group list/member-list reads. The app's Contacts data range must cover
every department and user group 小安工作助手 manages; otherwise group-member mapping
validation fails closed rather than publishing a partial permission snapshot.

## Rollout

### Resource ACL

`ENTERPRISE_ACL_ENABLED=false` keeps the legacy `is_public` rule active while
directory data and grants are populated. Before enabling it:

1. complete a successful directory and user-group sync;
2. compare department, employee, user-group, and group-member counts;
3. configure representative direct and group grants;
4. verify catalog, resolve, mention, Run, Schedule, worker pre-submit, and
   diagnosis results;
5. set `ENTERPRISE_ACL_ENABLED=true` and restart API, scheduler, and workers.

### Admin RBAC

1. keep `ENTERPRISE_RBAC_ENABLED=false` while migrations and roles are seeded;
2. assign Platform Owner to at least one active non-staff test user;
3. verify Admin Me, permission-specific navigation, and direct-URL 403s;
4. set `ENTERPRISE_RBAC_ENABLED=true` and restart the API;
5. retain at least one active `is_staff` break-glass account throughout rollout.

New resources are created with `access_mode=admin_only`. Fixed applications are
also created disabled until an administrator verifies the shipped renderer.

## Runtime topology

```text
React SPA
    │
    ├─ studio-api :8080
    ├─ studio-stream :8081
    ├─ studio-scheduler
    │    ├─ user schedule loop
    │    └─ directory + Feishu user-group sync loop
    ├─ studio-worker --provider=feishu_aily
    └─ studio-worker --provider=feishu_delivery

MySQL 5.7 stores business truth; Redis remains cache/queue/notification state.
```
