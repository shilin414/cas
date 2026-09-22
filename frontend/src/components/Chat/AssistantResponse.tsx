import { useEffect, useId, useRef, useState } from 'react';
import {
  CheckCircleOutlined,
  ExclamationCircleOutlined,
  LoadingOutlined,
  MinusCircleOutlined,
  RightOutlined,
} from '@ant-design/icons';
import type { ChatMessage } from '@/stores/useRunChatStore';
import { MarkdownWithArtifacts } from './ArtifactMarkdown';
import './AssistantResponse.css';

/** Provider progress is not a final answer. Keep it inspectable, not prominent. */
export default function AssistantResponse({ message }: { message: ChatMessage }) {
  const { status, processText, retryNotice, content, artifacts } = message;
  const pending = status === 'streaming';
  const hasProcess = Boolean(processText?.trim());
  const regionId = useId();
  const labelId = useId();
  // Live progress opens automatically; terminal transitions collapse it.
  // Manual toggles remain in effect within the same execution status.
  const [disclosure, setDisclosure] = useState({ status, expanded: pending });
  const expanded = disclosure.status === status ? disclosure.expanded : pending;
  const scrollRef = useRef<HTMLDivElement>(null);
  const followTail = useRef(true);

  useEffect(() => {
    const region = scrollRef.current;
    if (expanded && region && followTail.current) region.scrollTop = region.scrollHeight;
  }, [processText, expanded]);

  const stateLabel = pending
    ? retryNotice || '进行中'
    : status === 'failed' ? '未完成' : status === 'cancelled' ? '已取消' : '已完成';
  const stateIcon = pending ? <LoadingOutlined spin />
    : status === 'failed' ? <ExclamationCircleOutlined />
      : status === 'cancelled' ? <MinusCircleOutlined /> : <CheckCircleOutlined />;

  return (
    <div className="run-chat-response">
      {hasProcess ? (
        <div className={`run-chat-process${expanded ? ' run-chat-process--expanded' : ''}`}>
          <button
            type="button"
            className="run-chat-process__toggle"
            aria-expanded={expanded}
            aria-controls={regionId}
            onClick={(event) => {
              event.stopPropagation();
              followTail.current = true;
              setDisclosure({ status, expanded: !expanded });
            }}
          >
            <span className={`run-chat-process__icon${pending ? ' run-chat-process__icon--active' : ''}`} aria-hidden="true">{stateIcon}</span>
            <span id={labelId} className="run-chat-process__label">执行过程</span>
            <span className="run-chat-process__state" role="status">{stateLabel}</span>
            <RightOutlined className="run-chat-process__chevron" aria-hidden="true" />
          </button>
          <div
            id={regionId}
            ref={scrollRef}
            className="run-chat-process__body"
            role="region"
            aria-labelledby={labelId}
            hidden={!expanded}
            tabIndex={expanded ? 0 : -1}
            onScroll={(event) => {
              const el = event.currentTarget;
              followTail.current = el.scrollHeight - el.scrollTop - el.clientHeight < 32;
            }}
          >
            <p className="run-chat-process__caption">
              {pending ? '以下为执行过程，最终回复将在完成后展示。' : '以下为执行过程记录，不代表最终回复。'}
            </p>
            <div className="run-chat-process__text">{processText}</div>
          </div>
        </div>
      ) : pending ? (
        <div className="run-chat-response__waiting" role="status">
          <LoadingOutlined spin aria-hidden="true" />
          <span>{retryNotice || '正在准备回复'}</span>
        </div>
      ) : null}
      {!pending && content.trim() ? (
        <div className="run-chat-response__answer prose prose-sm dark:prose-invert max-w-none">
          <MarkdownWithArtifacts content={content} artifacts={artifacts} />
        </div>
      ) : status === 'done' && !artifacts?.length ? (
        <p className="run-chat-response__empty">本次执行未返回文字回复。</p>
      ) : null}
    </div>
  );
}
