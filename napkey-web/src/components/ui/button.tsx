import type { ComponentPropsWithoutRef, ReactNode } from 'react';
import { Link } from '@/i18n/navigation';

/**
 * Kieu dang theo master.css (.btn): radius 12px (rounded-lg), padding 11px 20px,
 * active translate-y-px, transition 150ms. Nut hero muon giu pill tron tuong
 * minh qua className (rounded-full) van ghi de duoc.
 */
const variants = {
  primary: 'bg-accent text-white hover:bg-brand-hover px-5 py-[11px] font-semibold',
  secondary:
    'border border-line bg-transparent text-fg hover:bg-surface-2 px-5 py-[11px] font-semibold',
  pill: 'bg-surface-hover text-fg hover:bg-surface-2 px-3.5 py-[7px] text-ui',
} as const;

type Variant = keyof typeof variants;

const shared =
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-lg ' +
  'transition-[background,border-color,color,transform] duration-150 ease-[var(--ease-smooth)] ' +
  'active:translate-y-px cursor-pointer ' +
  'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ' +
  'disabled:pointer-events-none disabled:opacity-50';

type ButtonProps = {
  variant?: Variant;
  children: ReactNode;
  className?: string;
};

export function Button({
  variant = 'primary',
  className = '',
  children,
  ...rest
}: ButtonProps & ComponentPropsWithoutRef<'button'>) {
  return (
    <button className={`${shared} ${variants[variant]} ${className}`} {...rest}>
      {children}
    </button>
  );
}

export function ButtonLink({
  href,
  variant = 'primary',
  className = '',
  children,
  ...rest
}: ButtonProps & { href: string } & Omit<ComponentPropsWithoutRef<'a'>, 'href'>) {
  const isExternal = href.startsWith('http') || href.startsWith('#');
  const cls = `${shared} ${variants[variant]} ${className}`;

  if (isExternal) {
    // Anchor trong trang (#...) khong roi tab nen khong can rel.
    const rel = href.startsWith('http') ? (rest.rel ?? 'noopener noreferrer') : rest.rel;
    return (
      <a href={href} className={cls} {...rest} rel={rel}>
        {children}
      </a>
    );
  }

  return (
    <Link href={href} className={cls} {...rest}>
      {children}
    </Link>
  );
}
