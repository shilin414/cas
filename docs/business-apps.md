# 业务应用配置与安全验收

代码位于internal/businessapps及业务应用HTTP层，复用应用中心、收藏、ACL和工作台，不进入AI Run/OCR执行链。

| renderer_key | 作用 |
| --- | --- |
| barcode-query | 20位条码的生产/流向查询 |
| material-query | 货号/69码物料查询 |
| oa-unlock | OA解锁 |
| oa-password | OA密码重置 |
| ldap-password | 目录密码修改 |
| oa-phone | 手机/办公电话/传真修改 |
| tpm-account | TPM解锁/锁定/重置 |

迁移0042初始化应用数据；实际应用可见/启用和权限需管理员核对，不把初始化等同全员授权。

## 后端变量（只在安全配置文件或Secret中）

```dotenv
BUSINESS_EBOAT_URL=https://eboat.hengan.com
BUSINESS_EBOAT_AUTH=
BUSINESS_EBOAT_USER=
BUSINESS_EBOAT_PASSWORD=
BUSINESS_EBOAT_TENANT=000000
BUSINESS_MATERIAL_URL=
BUSINESS_MATERIAL_AUTH=
BUSINESS_OA_UNLOCK_URL=
BUSINESS_OA_APP_ID=
BUSINESS_OA_SECRET=
BUSINESS_TPM_URL=
BUSINESS_TPM_USER=
BUSINESS_TPM_PASSWORD=
BUSINESS_ACCOUNT_MAP={}
BUSINESS_OPERATOR_IDS=
```

EBOAT填源站，其他地址按上游契约填写完整接口URL；认证值保留正确Basic/AppCode前缀。缺配置时明确显示未配置，不能伪装空查询成功。
ACCOUNT_MAP是平台users.id到人工核验工号的JSON映射，OPERATOR_IDS是允许代办的平台用户id列表。不得从浏览器展示名称推断工号权限；代办仍受应用ACL约束。

## 调用与分享

浏览器通过同源登录Session和CSRF调用/api/v2/applications/{id}/business/status、execute及相关查询/分享接口；精确路由见HTTP注册代码。
条码/物料结果支持相应分享与飞书转发，快照与投递状态使用Redis；这不等于允许分享密码/账号修改结果。
20位数字按字符串处理，避免JS精度损失。

## 敏感操作

用户确认、账号授权、审计成功都是写操作前置条件。上游失败/结果不确定不自动重试，避免重复变更真实账号。
LDAP历史上游把密码放入GET参数，相关代理日志必须关闭或脱敏查询串；不能将上游URL/响应正文直接写进排障报告。
上线验收优先使用专用测试账户/模拟上游；没有明确授权不重置真实密码、不锁定真实用户、不向真实群发送测试数据。
配置改动后重启API；模板没有提供任何真实业务凭据。
