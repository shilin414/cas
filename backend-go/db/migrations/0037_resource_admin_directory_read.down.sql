DELETE rp
FROM admin_role_permissions rp
JOIN admin_roles r ON r.id=rp.role_id AND r.code='resource_admin'
JOIN admin_permissions p ON p.id=rp.permission_id AND p.code='directory.read';
