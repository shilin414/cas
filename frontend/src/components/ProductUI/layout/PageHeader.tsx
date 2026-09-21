import type { ReactNode } from 'react';

export interface PageHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  eyebrow?: ReactNode;
  breadcrumb?: ReactNode;
  meta?: ReactNode;
  actions?: ReactNode;
  className?: string;
}

export function PageHeader({ title, description, eyebrow, breadcrumb, meta, actions, className = '' }: PageHeaderProps) {
  return (
    <header className={`product-page-header ${className}`.trim()}>
      {breadcrumb && <div className="product-page-header__breadcrumb">{breadcrumb}</div>}
      <div className="product-page-header__row">
        <div className="product-page-header__copy">
          {eyebrow && <div className="product-page-header__eyebrow">{eyebrow}</div>}
          <h1 className="product-page-header__title">{title}</h1>
          {description && <p className="product-page-header__description">{description}</p>}
          {meta && <div className="product-page-header__meta">{meta}</div>}
        </div>
        {actions && <div className="product-page-header__actions">{actions}</div>}
      </div>
    </header>
  );
}
