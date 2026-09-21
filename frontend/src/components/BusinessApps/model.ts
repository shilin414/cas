export type BusinessKey = 'barcode-query' | 'material-query' | 'oa-unlock' | 'oa-password' | 'ldap-password' | 'oa-phone' | 'tpm-account';
export interface Definition { title: string; eyebrow: string; description: string; hint: string; options: { label: string; value: string }[] }
export const appDefinitions: Record<BusinessKey, Definition> = {
 'barcode-query': { title: '条码信息查询', eyebrow: '商品追溯', description: '从一枚条码，查看生产信息与流向记录。', hint: '请输入或粘贴包装上的20位数字条码。支持键盘式扫码枪输入；不使用拍照识别。', options: [{ label: '生产信息', value: 'production' }, { label: '流向记录', value: 'flow' }] },
 'material-query': { title: '物料信息查询', eyebrow: '物料档案', description: '按货号或69码，快速查找物料资料。', hint: '货号与69码择一查询。69码为69开头的13位数字；结果以业务系统返回为准。', options: [{ label: '按货号', value: 'article' }, { label: '按69码', value: 'ean' }] },
 'oa-unlock': { title: 'OA账号解锁', eyebrow: '账号服务', description: '恢复被锁定的OA账号，不修改当前密码。', hint: '解锁后请使用原密码登录。如果密码已遗忘，请前往“修改OA密码”。', options: [] },
 'oa-password': { title: '修改OA密码', eyebrow: '账号安全', description: '为OA账号设置新的登录密码。', hint: '提交前请确认工号。新密码不写入浏览器缓存或操作日志，修改后请重新登录OA。', options: [] },
 'ldap-password': { title: '修改LDAP密码', eyebrow: '账号安全', description: '更新LDAP目录账号的登录密码。', hint: '可能影响使用同一目录账号的其他系统。请确认影响范围后提交。', options: [] },
 'oa-phone': { title: '修改OA手机号码', eyebrow: '个人资料', description: '更新OA中的手机号码与办公联系方式。', hint: '手机号为必填项。未填写的办公电话和传真不会发送给上游，不用于清空已有信息。', options: [] },
 'tpm-account': { title: 'TPM账号管理', eyebrow: '账号服务', description: '集中处理TPM账号的解锁、密码重置与锁定。', hint: '重置密码会同时解锁账号；锁定会阻止该账号登录。每次操作均需再次确认。', options: [{ label: '解锁账号', value: 'unlock' }, { label: '重置密码', value: 'reset' }, { label: '锁定账号', value: 'lock' }] },
};
export const isQuery = (key: BusinessKey) => key === 'barcode-query' || key === 'material-query';
export const needsPassword = (key: BusinessKey, action: string) => key === 'oa-password' || key === 'ldap-password' || (key === 'tpm-account' && action === 'reset');
export type FormValues = Record<string, string>;
export interface BusinessInput { action?: string; barcode?: string; query?: string; usercode?: string; password?: string; mobile?: string; officephone?: string; fax?: string; confirmed?: boolean }
export function buildInput(key: BusinessKey, action: string, values: FormValues): BusinessInput {
 if (key === 'barcode-query') return { action, barcode: values.barcode.trim() };
 if (key === 'material-query') return { action, query: values.query.trim() };
 const input: BusinessInput = { usercode: values.usercode?.trim() || '', confirmed: true };
 if (key === 'tpm-account') input.action = action;
 if (needsPassword(key, action)) input.password = values.password;
 if (key === 'oa-phone') { input.mobile = values.mobile?.trim(); if (values.officephone?.trim()) input.officephone = values.officephone.trim(); if (values.fax?.trim()) input.fax = values.fax.trim(); }
 return input;
}
export function displayResult(data: unknown): string {
 if (data == null || data === '' || (Array.isArray(data) && !data.length)) return '未查询到相关记录';
 if (typeof data === 'string') return data.replace(/\\r\\n|\\n|\r\n/g, '\n');
 return JSON.stringify(data, null, 2);
}
