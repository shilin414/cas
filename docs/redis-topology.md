# Redis deployment modes

The Go backend selects the Redis topology explicitly with `REDIS_MODE`.
Application services use one topology-neutral client, so API, stream, worker,
and scheduler processes share the same configuration in both modes.

## Development and test: standalone

```dotenv
REDIS_MODE=standalone
REDIS_HOST=127.0.0.1
REDIS_PORT=6379
REDIS_DATABASE=0
REDIS_PASSWORD=replace-me
```

`REDIS_DB` remains accepted as a backward-compatible fallback, but new
deployments should use `REDIS_DATABASE`.

## Production: cluster

```dotenv
REDIS_MODE=cluster
REDIS_DATABASE=0
REDIS_PASSWORD=replace-me
REDIS_CLUSTER_NODES=redis-1.example:6379,redis-2.example:6379,redis-3.example:6379
REDIS_CLUSTER_MAX_REDIRECTS=3
```

Redis Cluster does not support logical databases other than database 0. The
backend rejects cluster configuration with a non-zero database or an empty node
list at startup instead of failing later during queue or session operations.

## Connection settings

```dotenv
REDIS_CONNECT_TIMEOUT=10s
REDIS_TIMEOUT=5s
REDIS_POOL_TIMEOUT=3s
REDIS_POOL_SIZE=20
REDIS_MIN_IDLE_CONNS=5
REDIS_MAX_IDLE_CONNS=10
REDIS_KEY_PREFIX=xiaoan3
```

In cluster mode, go-redis applies the pool size per cluster node. Keep the same
key prefix across all backend processes in one environment. Credentials belong
in the deployment secret store or ignored `.env.local`, never in tracked files.
