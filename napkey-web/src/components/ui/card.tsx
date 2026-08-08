import type { ReactNode } from 'react';

/**
 * `interactive` chi bat hover khi the that su dan den mot hanh dong. The tinh
 * doi mau khi hover tao ky vong sai la co the bam duoc.
 */
export function Card({
  children,
  className = '',
  interactive = false,
}: {
  children: ReactNode;
  className?: string;
  interactive?: boolean;
}) {
  return (
    <div
      className={
        'rounded-lg border border-line bg-surface p-8 ' +
        (interactive
          ? 'transition-colors duration-300 ease-[var(--ease-smooth)] hover:bg-surface-hover '
          : '') +
        className
      }
    >
      {children}
    </div>
  );
}
