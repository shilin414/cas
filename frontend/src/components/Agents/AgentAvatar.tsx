/**
 * AgentAvatar — an Application's face, in one place.
 *
 * Every surface that lists or names an agent shows the same thing: the uploaded
 * avatar image when there is one, else the emoji icon (home shortcut cards,
 * the switcher, the chat empty state, message bubbles). Sizing/shape are the
 * caller's CSS; identity resolution stays in `lib/chatIdentity`.
 */
import React, { useEffect, useState } from 'react';
import { registerSessionReset } from '@/stores/resetSessionState';
import {
  agentAvatarFallback,
  agentAvatarUrl,
  agentDisplayName,
} from '@/lib/chatIdentity';
import type { ChatAgentIdentity } from '@/lib/chatIdentity';
import './AgentAvatar.css';

const decodedAvatarUrls = new Set<string>();
registerSessionReset(() => decodedAvatarUrls.clear());

interface Props {
  application?: ChatAgentIdentity | null;
  /** Rendered size in px (also drives the emoji font size). */
  size?: number;
  /** 'square' matches the 智能体市场 cards, 'circle' reads as a face. */
  shape?: 'square' | 'circle';
  className?: string;
  /** Optional brand colour tinted behind the emoji fallback. */
  tint?: string;
}

const AgentAvatar: React.FC<Props> = ({
  application, size = 34, shape = 'square', className, tint,
}) => {
  const url = agentAvatarUrl(application);
  const [imageFailed, setImageFailed] = useState(false);
  const [imageLoaded, setImageLoaded] = useState(() => Boolean(url) && decodedAvatarUrls.has(url));
  useEffect(() => {
    setImageFailed(false);
    setImageLoaded(Boolean(url) && decodedAvatarUrls.has(url));
  }, [url]);
  const showImage = Boolean(url) && !imageFailed;
  const revealImage = showImage && imageLoaded;
  const name = agentDisplayName(application);
  const style: React.CSSProperties = {
    width: size,
    height: size,
    fontSize: Math.round(size * 0.5),
  };
  // The tint only matters for the emoji fallback; an image covers it anyway.
  if (tint && !showImage) {
    style.background = `color-mix(in srgb, ${tint} 16%, transparent)`;
  }
  return (
    <span
      className={`agent-avatar agent-avatar--${shape} ${className || ''}`.trim()}
      style={style}
      title={name}
      data-agent-avatar={!showImage ? 'emoji' : revealImage ? 'image' : 'loading'}
    >
      {showImage && (
        <img
          className={`agent-avatar__img${revealImage ? ' is-loaded' : ''}`}
          src={url}
          alt={`${name} 头像`}
          loading="eager"
          decoding="async"
          onLoad={() => {
            decodedAvatarUrls.add(url);
            setImageLoaded(true);
          }}
          onError={() => {
            decodedAvatarUrls.delete(url);
            setImageLoaded(false);
            setImageFailed(true);
          }}
        />
      )}
      {!revealImage && (
        <span className="agent-avatar__emoji">{agentAvatarFallback(application)}</span>
      )}
    </span>
  );
};

export default AgentAvatar;
