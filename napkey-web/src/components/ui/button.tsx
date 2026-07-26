import type { ComponentPropsWithoutRef, ReactNode } from 'react';
import { Link } from '@/i18n/navigation';

/**
 * Button luon bo tron hoan toan (rounded-full) theo DESIGN.md muc 7.
 * Card bo 4-12px. Dung tron nguoc lai.
 */
const variants = {
  primary: 'bg-fg text-bg hover:bg-white/90 px-10 py-4',
  secondary: 'bg-surface-hover text-zinc-300 border border-line hover:bg-white/10 px-8 py-4',
  pill: 'bg-surface-hover text-fg hover:bg-white/10 px-6 py-2 text-ui',
} as const;

type Variant = keyof typeof variants;

const shared =
  'inline-flex items-center justify-center gap-2 rounded-full font-medium ' +
  'transition-colors duration-150 ease-[var(--ease-smooth)] ' +
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
    return (
      <a href={href} className={cls} {...rest}>
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
