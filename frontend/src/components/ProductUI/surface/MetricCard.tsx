import type { ReactNode } from 'react';
import { Surface } from './Surface';
export interface MetricCardProps { label: ReactNode; value: ReactNode; hint?: ReactNode; icon?: ReactNode; trend?: ReactNode; className?: string; }
export function MetricCard({ label, value, hint, icon, trend, className = '' }: MetricCardProps) {
  return <Surface as="article" tone="muted" className={`product-metric-card ${className}`.trim()}>{icon && <div className="product-metric-card__icon">{icon}</div>}<div className="product-metric-card__label">{label}</div><div className="product-metric-card__value">{value}</div>{hint && <div className="product-metric-card__hint">{hint}</div>}{trend && <div className="product-metric-card__trend">{trend}</div>}</Surface>;
}
