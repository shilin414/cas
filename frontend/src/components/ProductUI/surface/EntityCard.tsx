import type { ReactNode } from 'react';
import { Surface } from './Surface';

export interface EntityCardProps {
  leading?: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  meta?: ReactNode;
  trailing?: ReactNode;
  footer?: ReactNode;
  onClick?: () => void;
  className?: string;
  ariaLabel?: string;
}
export function EntityCard({ leading, title, description, meta, trailing, footer, onClick, className = '', ariaLabel }: EntityCardProps) {
  const main = <>{leading && <div className="product-entity-card__leading">{leading}</div>}<div className="product-entity-card__body"><h3>{title}</h3>{description && <p>{description}</p>}{meta && <div className="product-entity-card__meta">{meta}</div>}</div></>;
  return (
    <Surface as="article" tone="plain" interactive={Boolean(onClick)} className={`product-entity-card ${className}`.trim()}>
      <div className="product-entity-card__row">
        {onClick ? <button type="button" className="product-entity-card__action" onClick={onClick} aria-label={ariaLabel}>{main}</button> : <div className="product-entity-card__action">{main}</div>}
        {trailing && <div className="product-entity-card__trailing">{trailing}</div>}
      </div>
      {footer && (onClick ? (
        <button type="button" className="product-entity-card__footer product-entity-card__footer--action" onClick={onClick}>
          {footer}
        </button>
      ) : <div className="product-entity-card__footer">{footer}</div>)}
    </Surface>
  );
}
