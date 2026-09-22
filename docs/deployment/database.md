# MySQL 人工准备、SQL升级与首个管理员

**必须由DBA/发布人审核并手动执行。本文命令没有对生产运行。** 不在CI、服务启动、Kubernetes InitContainer/Job或Helm hook中执行生产数据库调整。
当前方言基线是MySQL5.7，迁移目录最高版本0045，共45个up文件。0045新增运行中心分页索引；本轮未执行，需要另行评审DDL与执行计划。

## 1. 操作前确认

核对实例、库、发布提交、当前schema版本、dirty状态、数据/文件备份与维护窗口。生产计划为192.168.212.165:23306 / xiaoan。初次部署与已有库升级不能混淆，不能向已有库重放0001。
数据库、加密密钥和存储备份需配套；已有库不能在发版时随机更换TOKEN_ENCRYPTION_KEY。

口令用提示输入，不写在命令行：


```bash
mysql --protocol=TCP -h 192.168.212.165 -P 23306 -u root -p
```

```sql
SELECT VERSION(), @@hostname, @@port;
SHOW DATABASES LIKE 'xiaoan';
-- 仅库已存在时：
USE xiaoan;
SHOW TABLES LIKE 'schema_migrations';
-- 仅迁移表已存在时：
SELECT version, dirty FROM schema_migrations;
```

若已有业务表但没有迁移表，先比对真实结构和历史，不直接伪造version=45。dirty=1时先处理失败变更。

## 2. 新空库和账户SQL

以下占位符替换后人工执行。用户host应匹配实际Pod/出口/DBA地址，不默认使用百分号全网授权。账户已存在时先SHOW GRANTS，不盲目重置密码。

```sql
CREATE DATABASE IF NOT EXISTS xiaoan
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'xiaoan_app'@'REPLACE_CLIENT_HOST'
  IDENTIFIED BY 'REPLACE_STRONG_RUNTIME_PASSWORD';
GRANT SELECT, INSERT, UPDATE, DELETE ON xiaoan.*
  TO 'xiaoan_app'@'REPLACE_CLIENT_HOST';
CREATE USER 'xiaoan_migrator'@'REPLACE_DBA_HOST'
  IDENTIFIED BY 'REPLACE_STRONG_MIGRATION_PASSWORD';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, INDEX, REFERENCES
  ON xiaoan.* TO 'xiaoan_migrator'@'REPLACE_DBA_HOST';
```

应用常驻使用runtime用户，DDL用户只用于维护窗口。现有本地prod配置使用root，是否切换专用账户由DBA确认，本次没有修改实际账号。

## 3. 备份

按已批准的一致性备份策略操作，备份目录需预先存在；口令不落命令行：

```bash
umask 077
mysqldump --protocol=TCP -h 192.168.212.165 -P 23306 -u REPLACE_BACKUP_USER -p \
  --single-transaction --hex-blob --default-character-set=utf8mb4 \
  --set-gtid-purged=OFF --no-tablespaces xiaoan > /secure-backup/xiaoan-before-release.sql
```

需要事件/触发器/存储过程时按实际添加选项与权限。必须验证可恢复性，不仅检查文件非空。保留BINARY(16)、UTC时间、JSON语义。

## 4. 生成SQL工件：只生成，不连接数据库

在发布提交的仓库根目录执行。设置FROM_VERSION为DBA确认的当前干净版本；**仅新空库使用0**。可在有Python3的构建机生成，再将工件交DBA，不依赖CentOS7系统Python。

生成器按编号合并权威迁移SQL，并在每个版本前后记录dirty状态；它不是数据库状态检测器，操作人仍需防止版本选错或并发发布。

```bash
export FROM_VERSION=0  # 改成已确认的当前版本，不要默认照抄0
umask 077
python3 - <<'PY'
from pathlib import Path
import os
start = int(os.environ['FROM_VERSION'])
files = sorted(Path('backend-go/db/migrations').glob('*.up.sql'))
versions = [int(f.name.split('_',1)[0]) for f in files]
assert versions == list(range(1,46)), '迁移目录已变化，重新审阅目标版本'
assert 0 <= start <= 45
pending = [f for f,v in zip(files,versions) if v > start]
assert pending, '没有待执行迁移'
parts = ['-- REVIEW BEFORE EXECUTION. Never use mysql --force.\n',
         'SET NAMES utf8mb4;\nSET time_zone = "+00:00";\n',
         'CREATE TABLE IF NOT EXISTS schema_migrations '
         '(version BIGINT NOT NULL PRIMARY KEY, dirty BOOLEAN NOT NULL) ENGINE=InnoDB;\n']
for f in pending:
    v = int(f.name.split('_',1)[0])
    parts += [f'\n-- BEGIN {f.name}\n',
              f'START TRANSACTION;\nDELETE FROM schema_migrations;\n'
              f'INSERT INTO schema_migrations(version,dirty) VALUES ({v},1);\nCOMMIT;\n',
              f.read_text(encoding='utf-8')+'\n',
              f'UPDATE schema_migrations SET dirty=0 WHERE version={v};\n']
parts += ['SELECT version,dirty FROM schema_migrations;\n']
Path('release-schema.sql').write_text(''.join(parts),encoding='utf-8')
print('Generated',len(pending),'migrations; NOT executed')
PY
```

完整DDL/DML来自[db/migrations](../../backend-go/db/migrations/)，不复制一份会漂移的表结构到文档。0042包含业务应用数据，0044包含无密钥GLM/OCR预设，0045包含可能对大表产生DDL压力的索引，审阅时不要只看建表。

## 5. 人工执行与验收

先暂停Scheduler和两个Worker，并停止API写入或进入维护窗口。确认备份、目标版本与SQL审阅通过后执行：

```bash
mysql --protocol=TCP -h 192.168.212.165 -P 23306 -u xiaoan_migrator -p \
  --default-character-set=utf8mb4 --batch xiaoan < release-schema.sql
# 检查退出码。禁止 --force；必须让遇错后停止并保留dirty状态。
```

```sql
USE xiaoan;
SELECT version, dirty FROM schema_migrations;
-- 本文基线期望44 / 0。
SHOW TABLES LIKE 'ai_models';
SHOW TABLES LIKE 'sync_target_configs';
SELECT COUNT(*) FROM users;
```

MySQL5.7 DDL不是整批事务。遇错不能直接清dirty继续：先核对失败语句、已落地DDL和备份，再决定修复或恢复。版本表只能反映确实完成的状态。

替代方案：DBA也可审核配置后**人工**执行项目cmd/migrate，它应用所有待执行版本并退出。不要同时用手工SQL与工具重复执行同一版本，也不把工具绑定CI或应用启动。

## 6. 首个管理员

ADMIN_BOOTSTRAP_PASSWORD当前不会自动创建管理员，没有文档约定的默认口令。
新库先完成飞书配置与OAuth白名单，让指定人员登录一次生成用户；DBA核验身份后提升该唯一用户：

```sql
SELECT id, username, display_name, auth_source, is_active, is_staff
FROM users ORDER BY id DESC LIMIT 20;
-- 仅替换为已核验的唯一数字id，不按模糊姓名提升整批用户。
UPDATE users SET is_staff=1, updated_at=UTC_TIMESTAMP(3)
WHERE id=REPLACE_VERIFIED_USER_ID AND is_active=1;
SELECT id, display_name, is_staff FROM users WHERE id=REPLACE_VERIFIED_USER_ID;
```

之后在企业管理中按职责配置RBAC。新空库没有现成的本地管理员密码；/login/admin仅适用于已有合法密码哈希的管理员。不得写明文、空口令或复制共享测试密码。
首次投产审查启用的计划、应用可见性与上游授权；不要用真实密码重置或向真实群补发历史消息来做普通验收。
