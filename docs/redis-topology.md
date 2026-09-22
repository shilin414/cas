# Redis 拓扑与版本兼容

后端通过 `internal/platform/redisx` 统一客户端，所有角色读取同一套配置。

## 单机

```dotenv
REDIS_MODE=standalone
REDIS_HOST=192.168.211.26
REDIS_PORT=6381
REDIS_DATABASE=2
REDIS_PASSWORD=REPLACE_SECRET
REDIS_KEY_PREFIX=xiaoan3
```

旧名 `REDIS_DB` 仍可用，`REDIS_DATABASE` 优先。不要混用互相冲突的两者。

## Cluster

```dotenv
REDIS_MODE=cluster
REDIS_DATABASE=0
REDIS_CLUSTER_NODES=192.168.212.165:6381,192.168.212.165:6382,192.168.212.165:6383
REDIS_CLUSTER_MAX_REDIRECTS=3
REDIS_PASSWORD=REPLACE_SECRET
REDIS_KEY_PREFIX=xiaoan-prod
```

Cluster 配置拒绝非 0 DB、空节点列表和非法 host:port。当前封装不暴露 Sentinel、Redis ACL username 或 Redis TLS 配置。
连接池 `REDIS_POOL_SIZE` 是每节点参数，部署多个 API/Worker 后应计算总连接数。

## Redis 5 回收

- Worker 先使用 `XAUTOCLAIM`；遇到该命令不支持后，当前 Worker 进程切换 `XPENDING + XCLAIM`。
- 不使用 Redis 6.2 的 XPENDING IDLE/排他范围语法；本地检查 idle，XCLAIM 再检查 min-idle。
- 一次操作一个 Stream Key；Token 缓存和刷新锁使用相同 hash tag，避免多 Key Lua 的跨槽问题。
- 回退不能解释成“任何 Redis 5 集群都已完整验收”。仍须在目标网络验证认证、被发现节点可达、实际消息回收、故障恢复和权限。

2026-09-21 本地记录：开发入口为 5.0.9；生产三个入口只读探测为 5.0.14、cluster_state=ok、known_nodes=6，且支持 XPENDING/XCLAIM。该记录不是实时状态，也不代表生产全链路或故障转移验收通过。

## 网络与数据迁移

Redis Cluster 的 seed 可达不等于全集群可达：客户端会跟随拓扑中的主节点/副本地址。容器到所有 advertised client ports 都需连通；集群内部 bus 端口由 Redis 运维管理，不必向应用开放。

切换 Redis 地址/前缀不会自动搬 Session、缓存或 Stream，用户可能需要重新登录。先暂停调度和消费、核对 Outbox/Pending、规划业务恢复后再切换，不要把旧队列直接丢弃当完成迁移。
禁止对共享环境执行 `FLUSHDB` / `FLUSHALL`。监控 evictions、队列 Pending、Outbox backlog、回退日志和业务终态；Ping 正常不是队列健康证明。
