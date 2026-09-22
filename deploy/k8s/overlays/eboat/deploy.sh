#!/usr/bin/env bash
# ============================================================
#  creation_agent_studio -> Kubernetes (eboat-ai-ns / Higress)
# ============================================================
# 这份脚本把文档里的手工步骤按顺序串起来，每一步失败即停。
# 它【不】做数据库迁移 —— SQL 必须由 DBA 按 docs/deployment/database.md 人工执行。
#
# 前置（必须先准备好，脚本不会替你造）：
#   1. /etc/xiaoan/backend.env   —— 由 backend.env.template 填写而来，权限 0600
#   2. /etc/xiaoan/storage.yaml  —— 由 storage.yaml.template 填写 StorageClass
#   3. harbor-pull secret        —— 见下面 create_pull_secret
#   4. xiaoan-tls secret         —— 启用 HTTPS 时才有
#
# 用法：
#   export RELEASE=prod-202609221200        # 你的镜像 tag
#   ./deploy.sh check                       # 只做前置检查
#   ./deploy.sh render                      # 渲染 + dry-run + diff
#   ./deploy.sh apply                       # 全量发布
# ============================================================
set -euo pipefail

NS=eboat-ai-ns
APP_ENV_FILE=${APP_ENV_FILE:-/etc/xiaoan/backend.env}
STORAGE_FILE=${STORAGE_FILE:-/etc/xiaoan/storage.yaml}
# 仓库根目录（本脚本在 deploy/k8s/overlays/eboat/ 下）
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
OVERLAY="deploy/k8s/overlays/eboat"
RENDERED=/etc/xiaoan/release.rendered.yaml

log()  { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m[warn] %s\033[0m\n' "$*"; }
die()  { printf '\033[1;31m[fail] %s\033[0m\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------
cmd_check() {
  log "确认当前 context / 权限（先确认不是错误集群）"
  kubectl version --output=yaml 2>/dev/null | grep -E 'gitVersion' | head -2 || true
  echo "context: $(kubectl config current-context)"
  kubectl get nodes -o wide

  log "IngressClass（必须是 higress）"
  kubectl get ingressclass

  log "StorageClass（找支持 RWX 的）"
  kubectl get storageclass

  log "namespace / 权限"
  kubectl get namespace "$NS" --show-labels 2>/dev/null \
    || warn "namespace $NS 不存在，需管理员创建：kubectl create namespace $NS"
  kubectl auth can-i create deployments -n "$NS"
  kubectl auth can-i create secrets -n "$NS"
  kubectl auth can-i get pods -n "$NS"
}

# ---------------------------------------------------------------
cmd_secrets() {
  [[ -f "$APP_ENV_FILE" ]] || die "缺少 $APP_ENV_FILE（从 backend.env.template 填写）"

  # 防呆：REPLACE_ 没替换干净就直接停。管道含密钥，不要开 shell tracing。
  if grep -q 'REPLACE_' "$APP_ENV_FILE"; then
    grep -n 'REPLACE_' "$APP_ENV_FILE"
    die "上面这些值还没替换"
  fi

  log "创建/更新 xiaoan-secrets（内容不落盘、不回显）"
  kubectl -n "$NS" create secret generic xiaoan-secrets \
    --from-env-file="$APP_ENV_FILE" \
    --dry-run=client -o yaml | kubectl apply -f -

  log "确认 Secret 键存在（只看键名，不打印值）"
  kubectl -n "$NS" get secret xiaoan-secrets -o jsonpath='{.data}' \
    | tr ',' '\n' | sed 's/[":{].*//' | grep -v '^$' | sort
}

# ---------------------------------------------------------------
cmd_create_pull_secret() {
  # 需要一个只含目标项目拉取权限的 dockerconfigjson。参数：文件路径
  local dcfg=${1:?用法: deploy.sh create_pull_secret /path/harbor-docker-config.json}
  log "创建 harbor-pull"
  kubectl -n "$NS" create secret generic harbor-pull \
    --type=kubernetes.io/dockerconfigjson \
    --from-file=.dockerconfigjson="$dcfg" \
    --dry-run=client -o yaml | kubectl apply -f -
}

# ---------------------------------------------------------------
cmd_storage() {
  [[ -f "$STORAGE_FILE" ]] || die "缺少 $STORAGE_FILE（从 storage.yaml.template 填写）"
  grep -q 'REPLACE_' "$STORAGE_FILE" && die "$STORAGE_FILE 里还有 REPLACE_ 没替换"
  log "申请 PVC（不随应用清单管理生命周期）"
  kubectl apply -f "$STORAGE_FILE"
  log "等待 Bound"
  kubectl -n "$NS" wait --for=jsonpath='{.status.phase}'=Bound \
    pvc/xiaoan-storage --timeout=120s \
    || die "PVC 未 Bound：检查 StorageClass 是否支持 RWX、CSI 是否正常"
}

# ---------------------------------------------------------------
cmd_render() {
  log "渲染清单"
  (cd "$ROOT" && kubectl kustomize "$OVERLAY") > "$RENDERED"

  log "检查残留占位符（有任何输出即停止）"
  if grep -n 'REPLACE_' "$RENDERED"; then
    die "渲染结果仍有 REPLACE_"
  fi

  log "服务端 dry-run"
  kubectl apply --dry-run=server -f "$RENDERED"

  log "diff（1=有差异，>1=出错）"
  kubectl diff -f "$RENDERED" || [[ $? -eq 1 ]] || die "kubectl diff 出错"
  echo "渲染文件：$RENDERED"
}

# ---------------------------------------------------------------
cmd_apply() {
  log "应用清单"
  (cd "$ROOT" && kubectl apply -k "$OVERLAY")

  log "逐角色等待就绪"
  for d in xiaoan-api xiaoan-stream xiaoan-worker-aily \
           xiaoan-worker-delivery xiaoan-scheduler xiaoan-web; do
    kubectl -n "$NS" rollout status "deployment/$d" --timeout=180s \
      || warn "$d 未在超时内就绪，去看 kubectl -n $NS describe deploy/$d"
  done

  kubectl -n "$NS" get pods,svc,ingress,pvc
}

# ---------------------------------------------------------------
cmd_verify() {
  log "健康检查"
  kubectl -n "$NS" exec deploy/xiaoan-api    -- wget -qO- http://127.0.0.1:8080/health/ready || warn "api not ready"
  kubectl -n "$NS" exec deploy/xiaoan-stream -- wget -qO- http://127.0.0.1:8081/health/ready || warn "stream not ready"
  kubectl -n "$NS" exec deploy/xiaoan-web    -- nginx -t                            || warn "nginx conf 有问题"

  log "Worker / Scheduler 没有 HTTP 探针，只能看日志"
  kubectl -n "$NS" logs deploy/xiaoan-worker-aily     --tail=50 || true
  kubectl -n "$NS" logs deploy/xiaoan-worker-delivery --tail=50 || true
  kubectl -n "$NS" logs deploy/xiaoan-scheduler       --tail=50 || true

  log "入口（DNS 需已指向 Higress）"
  echo "  https://ai.hengan.com/xiaoan-platform/"
}

# ---------------------------------------------------------------
cmd_restart() {
  # 固定名字的 Secret 更新不会自动重建 Pod，改 Secret 后必须显式重启。
  log "滚动重启全部后端角色"
  kubectl -n "$NS" rollout restart \
    deploy/xiaoan-api deploy/xiaoan-stream \
    deploy/xiaoan-worker-aily deploy/xiaoan-worker-delivery deploy/xiaoan-scheduler
}

case "${1:-}" in
  check)               cmd_check ;;
  secrets)             cmd_secrets ;;
  create_pull_secret)  shift; cmd_create_pull_secret "$@" ;;
  storage)             cmd_storage ;;
  render)              cmd_render ;;
  apply)               cmd_apply ;;
  verify)              cmd_verify ;;
  restart)             cmd_restart ;;
  *)
    cat <<'EOF'
用法: deploy.sh <command>

  check                 前置检查：context / IngressClass / StorageClass / 权限
  secrets               从 /etc/xiaoan/backend.env 生成 xiaoan-secrets
  create_pull_secret F  从 dockerconfigjson 生成 harbor-pull
  storage               申请 xiaoan-storage PVC 并等 Bound
  render                渲染 + 查占位符 + server dry-run + diff
  apply                 应用清单并等待各角色就绪
  verify                健康检查 + 看 Worker/Scheduler 日志
  restart               改过 Secret 后滚动重启后端

典型顺序：
  ./deploy.sh check && ./deploy.sh secrets && ./deploy.sh storage
  ./deploy.sh render && ./deploy.sh apply && ./deploy.sh verify
EOF
    exit 1 ;;
esac
