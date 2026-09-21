CREATE TABLE admin_roles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(100) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',
    is_system TINYINT(1) NOT NULL DEFAULT 0,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    created_by BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uniq_admin_role_code (code),
    KEY idx_admin_role_enabled (enabled, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE admin_permissions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(100) NOT NULL,
    category VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uniq_admin_permission_code (code),
    KEY idx_admin_permission_category (category, code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE admin_role_permissions (
    role_id BIGINT UNSIGNED NOT NULL,
    permission_id BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (role_id, permission_id),
    KEY idx_admin_role_permission_permission (permission_id, role_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE admin_role_assignments (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    role_id BIGINT UNSIGNED NOT NULL,
    scope_type VARCHAR(32) NOT NULL DEFAULT 'global',
    scope_id VARCHAR(128) NOT NULL DEFAULT '',
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    expires_at DATETIME(3) NULL,
    created_by BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uniq_admin_assignment (user_id, role_id, scope_type, scope_id),
    KEY idx_admin_assignment_user (user_id, enabled, expires_at),
    KEY idx_admin_assignment_role (role_id, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO admin_permissions(code, category, name, description) VALUES
('enterprise.overview.read','enterprise','查看企业概览','查看企业控制台概览'),
('resource.agent.read','resource','查看智能体','查看智能体资源'),
('resource.agent.manage','resource','管理智能体','创建和维护智能体资源'),
('resource.app.read','resource','查看应用','查看应用资源'),
('resource.app.manage','resource','管理应用','创建和维护应用资源'),
('resource.governance.read','resource','查看资源治理','查看资源治理信息'),
('resource.governance.manage','resource','管理资源治理','维护资源治理信息'),
('resource.access.bypass','resource','绕过资源 ACL','以平台维护身份访问资源'),
('access.policy.read','access','查看授权策略','查看资源授权策略'),
('access.policy.manage','access','管理授权策略','维护资源授权策略'),
('access.group.read','access','查看权限组','查看企业与本地权限组'),
('access.group.manage','access','管理权限组','维护本地权限组'),
('access.diagnosis.read','access','权限诊断','诊断用户资源访问路径'),
('directory.read','directory','查看组织目录','查看部门与人员'),
('directory.sync.read','directory','查看同步','查看目录同步状态'),
('directory.sync.manage','directory','管理同步','配置和触发目录同步'),
('admin.user.read','rbac','查看管理员','查看管理员及其角色'),
('admin.user.manage','rbac','管理管理员','分配和撤销管理员角色'),
('admin.role.read','rbac','查看角色','查看角色权限'),
('admin.role.manage','rbac','管理角色','创建和维护自定义角色'),
('provider.read','provider','查看 Provider','查看 Provider 状态'),
('provider.manage','provider','管理 Provider','维护 Provider 配置'),
('provider.test','provider','测试 Provider','执行 Provider 连通性测试'),
('runtime.read','runtime','查看 Runtime','查看 Runtime Binding'),
('runtime.manage','runtime','管理 Runtime','维护 Runtime Binding'),
('run.monitor.read','run','查看运行监控','查看企业运行状态'),
('run.manage','run','管理运行','执行运行管理动作'),
('audit.read','audit','查看审计','查看审计日志'),
('audit.export','audit','导出审计','导出审计日志'),
('settings.read','settings','查看设置','查看企业设置'),
('settings.manage','settings','管理设置','维护企业设置');

INSERT INTO admin_roles(code,name,description,is_system) VALUES
('platform_owner','Platform Owner','平台最高管理员',1),
('resource_admin','Resource Administrator','资源管理员',1),
('access_admin','Access Administrator','权限管理员',1),
('directory_admin','Directory Administrator','组织管理员',1),
('operations_admin','Operations Administrator','运行管理员',1),
('auditor','Auditor','审计员',1),
('readonly_admin','Readonly Administrator','只读管理员',1);

INSERT INTO admin_role_permissions(role_id, permission_id)
SELECT r.id,p.id FROM admin_roles r JOIN admin_permissions p
WHERE r.code='platform_owner';

INSERT INTO admin_role_permissions(role_id, permission_id)
SELECT r.id,p.id FROM admin_roles r JOIN admin_permissions p
WHERE r.code='resource_admin' AND p.code IN (
'enterprise.overview.read','resource.agent.read','resource.agent.manage','resource.app.read','resource.app.manage',
'resource.governance.read','resource.governance.manage','access.policy.read');

INSERT INTO admin_role_permissions(role_id, permission_id)
SELECT r.id,p.id FROM admin_roles r JOIN admin_permissions p
WHERE r.code='access_admin' AND p.code IN (
'enterprise.overview.read','resource.agent.read','resource.app.read','access.policy.read','access.policy.manage',
'access.group.read','access.group.manage','access.diagnosis.read');

INSERT INTO admin_role_permissions(role_id, permission_id)
SELECT r.id,p.id FROM admin_roles r JOIN admin_permissions p
WHERE r.code='directory_admin' AND p.code IN (
'enterprise.overview.read','directory.read','directory.sync.read','directory.sync.manage','access.group.read');

INSERT INTO admin_role_permissions(role_id, permission_id)
SELECT r.id,p.id FROM admin_roles r JOIN admin_permissions p
WHERE r.code='operations_admin' AND p.code IN (
'enterprise.overview.read','provider.read','provider.manage','provider.test','runtime.read','runtime.manage','run.monitor.read','run.manage');

INSERT INTO admin_role_permissions(role_id, permission_id)
SELECT r.id,p.id FROM admin_roles r JOIN admin_permissions p
WHERE r.code='auditor' AND p.code IN ('enterprise.overview.read','audit.read','audit.export');

INSERT INTO admin_role_permissions(role_id, permission_id)
SELECT r.id,p.id FROM admin_roles r JOIN admin_permissions p
WHERE r.code='readonly_admin' AND (p.code LIKE '%.read' OR p.code='run.monitor.read');
