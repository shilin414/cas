import type { ElementType, HTMLAttributes, ReactNode } from 'react';

export type SurfaceTone = 'plain' | 'raised' | 'muted';
export interface SurfaceProps extends HTMLAttributes<HTMLElement> {
  as?: ElementType;
  tone?: SurfaceTone;
  interactive?: boolean;
  children: ReactNode;
}
export function Surface({ as: Component = 'div', tone = 'plain', interactive = false, className = '', children, ...props }: SurfaceProps) {
  return <Component className={`product-surface product-surface--${tone}${interactive ? ' product-surface--interactive' : ''} ${className}`.trim()} {...props}>{children}</Component>;
}
