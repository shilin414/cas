import type { ReactNode } from 'react';

export interface PageToolbarProps {
  search?: ReactNode;
  filters?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  className?: string;
  ariaLabel?: string;
}

export function PageToolbar({ search, filters, actions, children, className = '', ariaLabel = '页面工具栏' }: PageToolbarProps) {
  return (
    <div className={`product-page-toolbar ${className}`.trim()} role="toolbar" aria-label={ariaLabel}>
      <div className="product-page-toolbar__primary">{search}{filters}{children}</div>
      {actions && <div className="product-page-toolbar__actions">{actions}</div>}
    </div>
  );
}
