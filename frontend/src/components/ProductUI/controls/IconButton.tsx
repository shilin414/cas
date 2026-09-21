import type { ButtonHTMLAttributes, ReactNode } from 'react';
export type IconButtonSize = 'sm' | 'md' | 'lg';
export interface IconButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> { label: string; icon: ReactNode; size?: IconButtonSize; tone?: 'default' | 'primary' | 'danger'; }
export function IconButton({ label, icon, size = 'md', tone = 'default', className = '', type = 'button', ...props }: IconButtonProps) { return <button type={type} className={`product-icon-button product-icon-button--${size} product-icon-button--${tone} ${className}`.trim()} aria-label={label} title={props.title ?? label} {...props}>{icon}</button>; }
