import type { ElementType, HTMLAttributes, ReactNode } from 'react';
export type PageWidth = 'compact' | 'default' | 'wide' | 'max' | 'full';
export interface PageSurfaceProps extends HTMLAttributes<HTMLElement> { as?: ElementType; width?: PageWidth; scroll?: boolean; padded?: boolean; children: ReactNode; }
export function PageSurface({ as: Component = 'section', width = 'default', scroll = false, padded = false, className = '', children, ...props }: PageSurfaceProps) { return <Component className={`product-page-surface product-page-surface--${width}${scroll ? ' product-page-surface--scroll' : ''}${padded ? '' : ' product-page-surface--flush'} ${className}`.trim()} {...props}><div className="product-page-surface__inner">{children}</div></Component>; }
