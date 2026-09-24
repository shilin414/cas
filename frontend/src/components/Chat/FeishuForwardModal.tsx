import { apiUrl } from '@/lib/deploymentPaths';
/**
 * FeishuForwardModal — pick Feishu users/groups and deliver a share snapshot
 * card with one unified user/group search, multi-select, recent-target history,
 * and per-target results.
 *
 * 数据面复用 useFeishuTargets（五次复审 P2-8）：搜索词直发后端（user 走
 * 目录搜索、chat 由后端翻全量后按名称过滤），请求带代际守卫 —— 旧实现
 * loadTargets 没有请求序号，慢的旧查询返回后会覆盖新查询的结果。
 *
 * Scope errors (403) surface a re-authorization action that reuses the
 * existing OAuth start → callback chain, which now requests the forwarding
 * scopes — that is how already-logged-in users top up permissions.
 *
 * Send session guard（八次复审 P1）：发送会话身份 = (open, shareToken)，
 * session epoch + single-flight + stale response guard —— 旧会话在途的发送
 * 响应对新会话 UI 一律无效。旧实现里 close → reopen 后，A 的迟到成功会把
 * 刚打开的 B 弹窗直接关掉，迟到失败会把 B 的选中目标覆盖成 A 的失败目
 * 标，且 B 会继承 A 的 sending 锁死发送按钮。
 *
 * Selection reconcile（九次复审 P1）：异步发送结果只从「当前选中」里移除
 * 本轮已成功的目标 —— snapshot 决定本次发了谁，current 决定用户现在想选
 * 谁；不得用发送开始时的快照整体覆盖 selected（用户在途期间取消的目标会
 * 复活、新选的目标会被删）。授权失效统一按 needsFeishuReauth 文案判断
 * （九次复审 P2）：200 per-target 授权失败与 HTTP 400/403 授权错误同一
 * 恢复入口，普通 400 不误判。
 *
 * Live interaction reconcile（十次复审 P1）：发送后的 toggle / search /
 * query 变更属于下一轮用户意图。全部成功只在没有新交互时自动关闭；
 * 已成功目标若在途被用户重新触碰（如取消后再选）则保留，不让旧请求的
 * 结果覆盖当前 picker 意图。
 */
import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Avatar, Empty, Input, Modal, Spin, message as antdMessage } from 'antd';
import { CheckOutlined, ReloadOutlined, SendOutlined } from '@ant-design/icons';
import {
  forwardShareToFeishu,
  loadForwardHistory,
  saveForwardHistory,
  type FeishuForwardTarget,
} from '@/services/shareApi';
import { useFeishuTargets } from '@/hooks/useFeishuTargets';
import './FeishuForwardModal.css';

interface FeishuForwardModalProps {
  open: boolean;
  shareToken: string | null;
  onClose: () => void;
  /** Optional delivery adapter; selection and retry behavior remain shared. */
  send?: typeof forwardShareToFeishu;
  summary?: React.ReactNode;
  reauthReturnTo?: string;
}

const HISTORY_LIMIT = 10;

/**
 * 前端 20 目标上限（七次复审 P2-8）：Backend feishuForwardMaxTargets=20、
 * OpenAPI maxItems=20，前端 toggle 必须同限 —— Chat 全量 + user 分页让用户
 * 更容易真实选到 21+，旧实现要点「发送（25）」才被后端 400 拒绝。
 */
const MAX_FORWARD_TARGETS = 20;

const targetKey = (
  target: Pick<FeishuForwardTarget, 'target_type' | 'id'>,
) => `${target.target_type}:${target.id}`;

interface SendFlight {
  epoch: number;
  interactionRevision: number;
  touchedTargetKeys: Set<string>;
}

/**
 * 统一的飞书授权失效判断（九次复审 P2）：授权相关文案分散在后端多处 ——
 * per-target 的「需要重新授权」「请重新绑定飞书账号」，HTTP 400 的「请先
 * 绑定飞书账号后使用转发」。按文案关键词判断而不是按 status：/forward
 * 的 400 还包括 invalid body / share not owned / revoked / targets>20 等
 * 非授权错误，不能全部当成 OAuth 问题处理。
 */
const needsFeishuReauth = (message?: string | null) => {
  if (!message) return false;
  return message.includes('重新授权')
    || message.includes('重新绑定飞书账号')
    || message.includes('绑定飞书账号')
    || message.includes('授权已过期');
};

const FeishuForwardModal: React.FC<FeishuForwardModalProps> = ({
  open,
  shareToken,
  onClose,
  send = forwardShareToFeishu,
  summary,
  reauthReturnTo,
}) => {
  const [query, setQuery] = useState('');
  const [needReauth, setNeedReauth] = useState(false);
  const [selected, setSelected] = useState<FeishuForwardTarget[]>([]);
  const [history, setHistory] = useState<FeishuForwardTarget[]>([]);
  const [sending, setSending] = useState(false);

  // 发送会话代际（八次复审 P1）：会话身份 = (open, shareToken)，任一变化
  // 即新会话 —— close → reopen 同一分享、A 分享 → B 分享都算换会话。与
  // Schedule Editor 的 editorEpochRef 同一套已验证模型：session epoch +
  // single-flight + stale response guard。
  const sessionEpochRef = useRef(0);
  // 同会话内的 picker 交互代际：区分「响应仍属于当前弹窗」与
  // 「响应是否仍能覆盖用户刚刚开始的下一轮操作」。
  const pickerInteractionRevisionRef = useRef(0);
  // single-flight：同会话内已有发送在途时，后续触发直接拒绝（双击发送只
  // 发一次 API 请求）。
  const sendInFlightRef = useRef<SendFlight | null>(null);

  // 会话失效是 session identity invalidation，须在 commit 后同步完成
  // （useLayoutEffect 而非 useEffect）：新会话打开的那一帧 sending 已复
  // 位、旧 in-flight 已作废，不留给旧响应操纵新会话 UI 的窗口。
  useLayoutEffect(() => {
    sessionEpochRef.current += 1;
    pickerInteractionRevisionRef.current = 0;
    sendInFlightRef.current = null;
    setSending(false);
    return () => { sessionEpochRef.current += 1; sendInFlightRef.current = null; };
  }, [open, shareToken]);

  useEffect(() => {
    if (!open) return;
    setHistory(loadForwardHistory());
  }, [open]);

  // 用户与群聊共用一个搜索框：群聊在打开时拉取全量并本地过滤，
  // 联系人只在输入关键词后调用飞书目录搜索。两个数据源同时启用，避免
  // 用户先选类型、再搜索的额外步骤；sessionKey 保证关闭重开不闪旧结果。
  const normalizedQuery = query.trim();
  const targetSessionKey = open ? `feishu-forward:${shareToken ?? ''}` : null;
  const chats = useFeishuTargets({
    type: 'chat',
    enabled: open,
    query: normalizedQuery,
    sessionKey: targetSessionKey,
  });
  const users = useFeishuTargets({
    type: 'user',
    enabled: open,
    query: normalizedQuery,
    sessionKey: targetSessionKey,
  });

  const recent = useMemo(
    () => history.slice(0, HISTORY_LIMIT),
    [history],
  );
  const recentKeys = useMemo(
    () => new Set(recent.map(targetKey)),
    [recent],
  );
  const targets = useMemo(() => {
    const seen = new Set<string>();
    return [...chats.items, ...users.items].filter((target) => {
      const key = targetKey(target);
      if (seen.has(key) || recentKeys.has(key)) return false;
      seen.add(key);
      return true;
    });
  }, [chats.items, users.items, recentKeys]);

  // 空查询时联系人数据源按契约保持 idle，只有群聊参与；输入关键词后两
  // 个数据源共同参与。只有全部参与的数据源都失败且没有可展示结果时才
  // 显示整屏错误，单源失败不能吞掉另一类已经加载成功的目标。
  const participatingSources = normalizedQuery ? [chats, users] : [chats];
  const sourceErrors = participatingSources.filter((source) => source.error);
  const blockingError = targets.length === 0
    && sourceErrors.length === participatingSources.length
    ? sourceErrors.map((source) => source.error).filter(Boolean).join('；')
    : null;
  const partialError = sourceErrors.length > 0
    && sourceErrors.length < participatingSources.length
    ? '部分搜索结果加载失败'
    : null;
  const loading = participatingSources.some((source) => source.loading);
  const retryFailedSources = () => Promise.all(
    sourceErrors.map((source) => source.refresh()),
  );

  // 任一数据源出现授权错误都进入统一恢复入口；联系人分页 403 也属于
  // 授权变化，不能因为群聊仍可见就隐藏重新授权操作。
  const authError = [chats, users].some((source) => (
    source.errorStatus === 403
    || source.errorStatus === 400
    || source.loadMoreErrorStatus === 403
  ));
  const showReauth = needReauth || authError;

  const reset = () => {
    setQuery('');
    setSelected([]);
    setNeedReauth(false);
  };

  const close = () => {
    reset();
    onClose();
  };

  const markPickerInteraction = () => {
    pickerInteractionRevisionRef.current += 1;
  };

  // toggle 是唯一的选中入口（列表行 + 最近转发 chip 都走它，七次复审
  // §35），上限写在 toggle 里两个入口同时生效。
  const toggle = (t: FeishuForwardTarget) => {
    markPickerInteraction();
    const flight = sendInFlightRef.current;
    if (flight?.epoch === sessionEpochRef.current) {
      flight.touchedTargetKeys.add(targetKey(t));
    }
    if (selected.some((x) => targetKey(x) === targetKey(t))) {
      setSelected(selected.filter((x) => targetKey(x) !== targetKey(t)));
      return;
    }
    if (selected.length >= MAX_FORWARD_TARGETS) {
      antdMessage.warning('一次最多转发给 20 个目标');
      return;
    }
    setSelected([...selected, t]);
  };

  const handleSend = async () => {
    const epoch = sessionEpochRef.current;
    if (!shareToken || !selected.length
      || sendInFlightRef.current?.epoch === epoch) {
      return;
    }
    const flight: SendFlight = {
      epoch,
      interactionRevision: pickerInteractionRevisionRef.current,
      touchedTargetKeys: new Set<string>(),
    };
    sendInFlightRef.current = flight;

    // snapshot（八次复审 §21）：API 请求、历史、失败目标 reconcile 全部
    // 基于同一批发送目标，不混用发送结束时可能已经变化的 selected /
    // shareToken（闭包里虽是旧值，显式命名让这个不变式可见）。
    const selectedSnapshot = selected;
    const shareTokenSnapshot = shareToken;

    setSending(true);
    try {
      const result = await send(
        shareTokenSnapshot,
        selectedSnapshot.map((t) => ({ target_type: t.target_type, id: t.id })),
      );
      // 旧会话的迟到响应对新会话 UI 一律无效，直接丢弃（八次复审 §22）：
      // 不能 message / setSelected / setNeedReauth / close / setSending ——
      // 这些副作用已经不属于当前 session。后端转发本身无法撤销，守卫的
      // 边界是「旧响应不操纵新会话的 UI」。
      if (sessionEpochRef.current !== epoch) return;

      const succeededIds = new Set(
        result.results.filter((r) => r.ok).map((r) => r.target_id),
      );
      if (succeededIds.size) {
        saveForwardHistory(
          selectedSnapshot.filter((t) => succeededIds.has(t.id)),
        );
      }
      const succeededKeys = new Set(
        selectedSnapshot
          .filter((t) => succeededIds.has(t.id))
          .map(targetKey),
      );
      // 全成功 / 部分失败共用同一套 live-edit reconcile：只移除
      // 本轮成功且发送后未被用户再次触碰的目标。
      setSelected((current) => current.filter((t) => (
        !succeededKeys.has(targetKey(t))
        || flight.touchedTargetKeys.has(targetKey(t))
      )));

      if (result.fail_count === 0) {
        antdMessage.success(`已发送给 ${result.success_count} 个目标`);
        const pickerChanged = pickerInteractionRevisionRef.current
          !== flight.interactionRevision;
        if (!pickerChanged) {
          close();
        }
      } else {
        const firstError = result.results.find((r) => !r.ok)?.error;
        antdMessage.error(
          `${result.success_count} 个成功，${result.fail_count} 个失败${firstError ? `：${firstError}` : ''}`,
        );
        // 重新授权看「是否存在授权失败」而不是「是否全部失败」（八次复审
        // P2）：1 成功 + 1 授权失败的混合结果同样要给重新授权入口 —— 旧
        // 条件 success_count === 0 让用户只能对着注定失败的目标反复重试。
        const authFailure = result.results.find(
          (r) => !r.ok && needsFeishuReauth(r.error),
        );
        if (authFailure) {
          setNeedReauth(true);
        }
      }
    } catch (e: any) {
      if (sessionEpochRef.current !== epoch) return;
      const detail = e?.response?.data?.detail
        || e?.response?.data?.error
        || '转发失败，请稍后重试';
      // HTTP 级授权失效（九次复审 P2）：POST /forward 的 400「请先绑定飞书
      // 账号后使用转发」此前只 toast，用户没有恢复入口 —— 目标列表早先加
      // 载成功不代表发送时 token 还有效。统一走 needsFeishuReauth，与 200
      // per-target 授权失败同一入口；普通 400（分享不存在 / body 非法）不
      // 误判。
      if (needsFeishuReauth(detail)) {
        setNeedReauth(true);
      }
      antdMessage.error(detail);
    } finally {
      // 会话已切换时 sending 已由 useLayoutEffect 复位、in-flight 已被清
      // 空，都不归这个旧 closure 管。
      if (sessionEpochRef.current === epoch) {
        setSending(false);
      }
      if (sendInFlightRef.current?.epoch === epoch) {
        sendInFlightRef.current = null;
      }
    }
  };

  const handleReauth = () => {
    const returnTo = reauthReturnTo || (window.location.pathname + window.location.search);
    window.location.href = apiUrl(`/identity/oauth/start?return_to=${encodeURIComponent(returnTo)}`);
  };

  return (
    <Modal
      open={open}
      onCancel={close}
      footer={null}
      title="转发到飞书"
      width={460}
      centered
      rootClassName="ffm-modal"
      destroyOnHidden
    >
      {showReauth ? (
        <div className="ffm-reauth">
          <p>当前飞书授权缺少转发所需权限（IM / 通讯录）。</p>
          <p className="ffm-reauth__hint">
            点击下方按钮重新授权后，回到本页再次发起转发即可。
          </p>
          <button className="ffm-reauth__btn" onClick={handleReauth}>
            <ReloadOutlined /> 重新授权飞书
          </button>
        </div>
      ) : (
        <div className="ffm-body">
          {summary && <div className="ffm-summary">{summary}</div>}
          <Input
            placeholder="搜索用户或群聊"
            value={query}
            allowClear
            onChange={(e) => {
              markPickerInteraction();
              setQuery(e.target.value);
            }}
          />

          {recent.length > 0 && (
            <div className="ffm-recent">
              <span className="ffm-recent__label">最近转发</span>
              <div className="ffm-recent__chips">
                {recent.map((t) => (
                  <button
                    key={`${t.target_type}-${t.id}`}
                    className={`ffm-recent-chip ${selected.some((x) => targetKey(x) === targetKey(t)) ? 'ffm-recent-chip--on' : ''}`}
                    title={t.name}
                    onClick={() => toggle(t)}
                  >
                    {t.name} · {t.target_type === 'chat' ? '群聊' : '联系人'}
                  </button>
                ))}
              </div>
            </div>
          )}

          <div className="ffm-list">
            {partialError && (
              <div className="ffm-partial-error">
                <span>{partialError}</span>
                <button type="button" onClick={() => void retryFailedSources()}>重试</button>
              </div>
            )}
            {/* 首页加载失败 ≠ 没有目标（五次复审 §28，ERROR ≠ EMPTY）：错误
                可见 + 可重试，不再静默空列表。分页失败（loadMoreError）不进
                这个分支 —— 已加载的行保持可见，只在底部给重试（七次复审
                P1-2）。 */}
            {blockingError ? (
              <div className="ffm-list__center ffm-list__error">
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={blockingError}
                />
                <button type="button" className="ffm-error-retry" onClick={() => void retryFailedSources()}>
                  <ReloadOutlined /> 重试
                </button>
              </div>
            ) : loading && targets.length === 0 ? (
              <div className="ffm-list__center"><Spin /></div>
            ) : targets.length === 0 ? (
              <div className="ffm-list__center">
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={normalizedQuery
                    ? '没有匹配的用户或群聊'
                    : '输入名称搜索用户，或从群聊中选择'}
                />
              </div>
            ) : (
              <>
                {targets.map((t) => {
                  const on = selected.some((x) => targetKey(x) === targetKey(t));
                  return (
                    <button
                      key={targetKey(t)}
                      className={`ffm-row ${on ? 'ffm-row--on' : ''}`}
                      onClick={() => toggle(t)}
                    >
                      <Avatar size={32} src={t.avatar_url || undefined}>
                        {(t.name || '?').slice(0, 1)}
                      </Avatar>
                      <span className="ffm-row__name" title={t.name}>{t.name}</span>
                      <span className="ffm-row__type">
                        {t.target_type === 'chat' ? '群聊' : '联系人'}
                      </span>
                      <span className={`ffm-row__check ${on ? 'ffm-row__check--on' : ''}`}>
                        {on && <CheckOutlined />}
                      </span>
                    </button>
                  );
                })}
                {/* 联系人 cursor 分页（六次复审 P1-3）：匹配的第 51+ 人靠
                    续拉补齐 —— 旧的「一页 20 条」让第 21 人永远选不到；
                    chat 恒全量，不显示此入口。分页失败时与「重试加载」互
                    斥（八次复审 P2）：两个按钮调的都是 loadMore，同时出
                    现只是两个并排的重复动作入口。 */}
                {users.hasMore && !users.loadMoreError && (
                  <button
                    type="button"
                    className="ffm-load-more"
                    disabled={users.loadingMore}
                    onClick={() => void users.loadMore()}
                  >
                    {users.loadingMore ? '加载中…' : '加载更多联系人'}
                  </button>
                )}
                {/* 分页失败 ≠ 整个数据源失败（七次复审 P1-2）：已加载的人
                    仍然有效，只提示「更多加载失败」+ 重试失败的那一页 ——
                    重试调 loadMore（从断点续拉），不是 refresh（回第一页
                    丢掉全部进度）。 */}
                {users.loadMoreError && (
                  <div className="ffm-paging-error">
                    <span className="ffm-paging-error__text">
                      {users.loadMoreError || '更多联系人加载失败'}
                    </span>
                    <button
                      type="button"
                      className="ffm-paging-error__retry"
                      disabled={users.loadingMore}
                      onClick={() => void users.loadMore()}
                    >
                      <ReloadOutlined /> 重试加载
                    </button>
                  </div>
                )}
              </>
            )}
          </div>

          <div className="ffm-footer">
            <button className="ffm-cancel" onClick={close}>取消</button>
            <button
              className="ffm-send"
              disabled={!selected.length || sending}
              onClick={() => void handleSend()}
            >
              <SendOutlined /> {sending ? '发送中…' : `发送（${selected.length}）`}
            </button>
          </div>
        </div>
      )}
    </Modal>
  );
};

export default FeishuForwardModal;
