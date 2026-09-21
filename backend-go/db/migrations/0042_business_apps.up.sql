-- Existing four application rows and their ACLs are intentionally untouched.
-- New sensitive applications are opt-in through the existing application ACL editor.
INSERT IGNORE INTO applications
 (slug,name,description,icon,color,kind,renderer_key,category_id,is_public,access_mode,is_default_agent,enabled,usage_count,created_by,organization_id)
SELECT 'ldap-password','修改LDAP密码','为已绑定的目录账号设置新密码','🔐','#7c3aed','form','ldap-password',
 (SELECT id FROM application_categories WHERE slug='apps'),0,'admin_only',0,1,0,NULL,NULL
UNION ALL
SELECT 'oa-phone','修改OA手机号码','更新OA手机号码和办公联系方式','📱','#059669','form','oa-phone',
 (SELECT id FROM application_categories WHERE slug='apps'),0,'admin_only',0,1,0,NULL,NULL
UNION ALL
SELECT 'tpm-account','TPM账号管理','解锁、锁定TPM账号或重置密码','🛡️','#d97706','form','tpm-account',
 (SELECT id FROM application_categories WHERE slug='apps'),0,'admin_only',0,1,0,NULL,NULL;
