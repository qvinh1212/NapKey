import type { ComponentPropsWithoutRef } from 'react';

/**
 * O nhap co nhan va thong bao loi.
 *
 * Tach ra vi bon form auth (dang nhap, dang ky, dat lai mat khau, doi mat khau)
 * dung lai dung mot cau truc, va phan noi loi voi o nhap bang aria-describedby la
 * cho de sai neu viet lai nhieu lan.
 */
export function Field({
  id,
  label,
  error,
  hint,
  ...rest
}: {
  id: string;
  label: string;
  error?: string;
  hint?: string;
} & ComponentPropsWithoutRef<'input'>) {
  const errorId = `${id}-error`;
  const hintId = `${id}-hint`;

  return (
    <div>
      <label htmlFor={id} className="mb-2 block text-ui text-muted">
        {label}
      </label>
      <input
        id={id}
        aria-invalid={error ? 'true' : undefined}
        aria-describedby={error ? errorId : hint ? hintId : undefined}
        className="w-full rounded-md border border-line bg-surface-hover px-4 py-2.5 text-ui text-fg placeholder:text-dim focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        {...rest}
      />
      {error ? (
        <p id={errorId} className="mt-1.5 text-ui text-danger">
          {error}
        </p>
      ) : hint ? (
        <p id={hintId} className="mt-1.5 text-ui text-dim">
          {hint}
        </p>
      ) : null}
    </div>
  );
}