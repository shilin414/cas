import type { HTMLAttributes, ReactNode } from 'react';

export interface ContentSectionProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}

export function ContentSection({ title, description, actions, children, className = '', ...props }: ContentSectionProps) {
  return (
    <section className={`product-content-section ${className}`.trim()} {...props}>
      {(title || description || actions) && (
        <div className="product-content-section__header">
          <div>
            {title && <h2 className="product-content-section__title">{title}</h2>}
            {description && <p className="product-content-section__description">{description}</p>}
          </div>
          {actions && <div className="product-content-section__actions">{actions}</div>}
        </div>
      )}
      <div className="product-content-section__body">{children}</div>
    </section>
  );
}
