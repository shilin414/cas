import type { ReactNode } from 'react';
import { Surface } from './Surface';
export interface DataPanelProps { title?: ReactNode; description?: ReactNode; toolbar?: ReactNode; children: ReactNode; footer?: ReactNode; className?: string; }
export function DataPanel({ title, description, toolbar, children, footer, className = '' }: DataPanelProps) {
  return <Surface as="section" className={`product-data-panel ${className}`.trim()}>
    {(title || description || toolbar) && <div className="product-data-panel__header"><div>{title && <h2>{title}</h2>}{description && <p>{description}</p>}</div>{toolbar && <div className="product-data-panel__toolbar">{toolbar}</div>}</div>}
    <div className="product-data-panel__content">{children}</div>{footer && <div className="product-data-panel__footer">{footer}</div>}
  </Surface>;
}
