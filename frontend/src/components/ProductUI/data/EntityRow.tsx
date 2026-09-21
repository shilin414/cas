import type { ReactNode } from 'react';
export interface EntityRowProps { leading?: ReactNode; title: ReactNode; description?: ReactNode; meta?: ReactNode; trailing?: ReactNode; onClick?: () => void; className?: string; }
export function EntityRow({ leading, title, description, meta, trailing, onClick, className = '' }: EntityRowProps) {
  const content = <>{leading && <div className="product-entity-row__leading">{leading}</div>}<div className="product-entity-row__body"><div className="product-entity-row__title">{title}</div>{description && <div className="product-entity-row__description">{description}</div>}{meta && <div className="product-entity-row__meta">{meta}</div>}</div>{trailing && <div className="product-entity-row__trailing">{trailing}</div>}</>;
  return onClick ? <button type="button" className={`product-entity-row product-entity-row--interactive ${className}`.trim()} onClick={onClick}>{content}</button> : <div className={`product-entity-row ${className}`.trim()}>{content}</div>;
}
