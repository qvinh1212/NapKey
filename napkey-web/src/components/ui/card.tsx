import type { ReactNode } from 'react';

export function Card({
  children,
  className = '',
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={
        'rounded-lg border border-line bg-surface p-8 ' +
        'transition-colors duration-300 ease-[var(--ease-smooth)] hover:bg-surface-hover ' +
        className
      }
    >
      {children}
    </div>
  );
}
