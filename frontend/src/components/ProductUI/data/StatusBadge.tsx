import type { ReactNode } from 'react';
export type StatusTone = 'success' | 'warning' | 'danger' | 'info' | 'neutral';
export function StatusBadge({ tone = 'neutral', children, className = '' }: { tone?: StatusTone; children: ReactNode; className?: string }) { return <span className={`product-status-badge product-status-badge--${tone} ${className}`.trim()}>{children}</span>; }
