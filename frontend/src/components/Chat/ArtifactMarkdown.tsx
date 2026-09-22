import { apiUrl, deploymentAssetUrl } from '@/lib/deploymentPaths';
/**
 * Shared artifact-aware markdown rendering (chat + public share page).
 *
 * Aily embeds generated files as `artifacts/<artifactName>/<...>/<filename>`
 * sandbox-relative refs; they mean nothing outside the chat surface until
 * rewritten to an /open resolver that 302s to a fresh provider signed URL.
 * Callers choose the resolver via `openArtifactUrl` — the in-app chat uses
 * the authenticated endpoint, the public share page the token-scoped one.
 * File bytes are always fetched by the browser straight from the provider.
 */
import React, { createContext, useCallback, useContext, useMemo, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import './ArtifactMarkdown.css';

import { artifactReferences, resolveArtifactRef } from './artifactRef';

const API_BASE = apiUrl();
const MARKDOWN_PLUGINS = [remarkGfm];

// Keep the table renderer stable across streaming updates, like artifact images.
function MarkdownTable({ children }: { children?: React.ReactNode }) {
  return (
    <div className="artifact-markdown__table-scroll" role="region" aria-label="表格，可横向滚动" tabIndex={0}>
      <table>{children}</table>
    </div>
  );
}

/**
 * Stable chat image. A provider artifact URL goes through the /open 302
 * resolver (24h URL, then CDN), which is slow on first load and must not be
 * re-fetched on every store update. Keying by src and keeping load state
 * here means a re-render only recomputes the frame, never restarts the
 * download; the placeholder avoids the browser's broken-image icon while
 * the 302 resolves.
 */
interface ChatImageProps {
  src: string;
  alt?: string;
}

// Reset both success and failure on source changes, including direct callers.
// Unchanged sources keep the same subtree during streaming text updates.
export const ChatImage: React.FC<ChatImageProps> = props => (
  <ChatImageContent key={props.src} {...props} />
);

const ChatImageContent: React.FC<ChatImageProps> = ({ src, alt }) => {
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  if (failed) {
    return <span className="chat-img chat-img--broken" title={alt}>{alt ? `${alt}：` : ''}图片暂不可用（上游未提供该文件或下载失败）</span>;
  }
  return (
    <span className="chat-img">
      {!loaded && (
        <span className="chat-img__loading" role="status" aria-label="图片加载中">
          <span className="chat-img__spinner" aria-hidden="true" />
        </span>
      )}
      <img
        src={src}
        alt={alt || ''}
        loading="lazy"
        onLoad={() => setLoaded(true)}
        onError={() => setFailed(true)}
      />
    </span>
  );
};

export interface ArtifactRef {
  artifactId: string;
  name: string;
}

const ArtifactRenderContext = createContext<{
  resolveArtifactSrc: (src: string) => string | null;
  pending: boolean;
}>({ resolveArtifactSrc: () => null, pending: false });

function MarkdownImage({ src, alt }: { src?: string; alt?: string }) {
  const { resolveArtifactSrc, pending } = useContext(ArtifactRenderContext);
  if (typeof src !== 'string') return <img src={src} alt={alt || ''} loading="lazy" />;
  if (/^(https?:|data:|blob:|\/api\/)/i.test(src)) {
    return <ChatImage src={deploymentAssetUrl(src)} alt={alt} />;
  }
  const resolved = resolveArtifactSrc(src);
  return resolved
    ? <ChatImage src={resolved} alt={alt} />
    : <span className={'chat-img chat-img--' + (pending ? 'pending' : 'broken')} title={src}>{pending ? (alt || '生成产物') : (alt || '图片') + '：回复未提供可用的文件引用'}</span>;
}

function MarkdownLink({ href, children, node: _node, ...rest }: any) {
  const { resolveArtifactSrc } = useContext(ArtifactRenderContext);
  return (
    <a
      {...rest}
      href={typeof href === 'string' ? (resolveArtifactSrc(href) ?? deploymentAssetUrl(href)) : href}
      target="_blank"
      rel="noreferrer"
    >{children}</a>
  );
}

const MARKDOWN_COMPONENTS = { table: MarkdownTable, img: MarkdownImage, a: MarkdownLink };

export const MarkdownWithArtifacts: React.FC<{
  content: string;
  artifacts?: ArtifactRef[];
  /** Override the resolver target (public shares use the token-scoped one). */
  openArtifactUrl?: (artifactId: string) => string;
  pending?: boolean;
}> = ({ content, artifacts, openArtifactUrl, pending = false }) => {
  const defaultResolveUrl = useCallback(
    (artifactId: string) => `${API_BASE}/v2/artifacts/${artifactId}/open`,
    [],
  );
  const resolveUrl = openArtifactUrl ?? defaultResolveUrl;
  const referencesKey = JSON.stringify(artifactReferences(content));
  const references = useMemo(() => JSON.parse(referencesKey) as string[], [referencesKey]);

  /**
   * Matched /open URL, or null when the ref is still unresolved. The matching
   * rules live in ./artifactRef so they can be unit-tested: a nested
   * `artifacts/<name>/<sub>/<file>` ref used to resolve to nothing, which is
   * why only the first of two generated images appeared.
   */
  const resolveArtifactSrc = useCallback(
    (src: string): string | null => resolveArtifactRef(src, artifacts, resolveUrl, references),
    [artifacts, resolveUrl, references]);

  // New artifacts update resolver data, not renderer identity: already loaded
  // image components must survive sibling discovery without another download.
  const renderContext = useMemo(() => ({ resolveArtifactSrc, pending }), [resolveArtifactSrc, pending]);

  return (
    <ArtifactRenderContext.Provider value={renderContext}>
      <div className="artifact-markdown">
        <ReactMarkdown remarkPlugins={MARKDOWN_PLUGINS} components={MARKDOWN_COMPONENTS}>
          {content}
        </ReactMarkdown>
      </div>
    </ArtifactRenderContext.Provider>
  );
};
