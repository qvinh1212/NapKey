import type { ReactNode } from 'react';

/**
 * Primitive dung trong console.
 *
 * Console la mat khac cua landing: day thong tin, chu nho (13px theo DESIGN.md muc
 * 7), khong hieu ung. Nen cac primitive o day khong dung lai `Card` cua landing -
 * card landing co padding p-8 va hieu ung hover, ca hai deu sai cho mot bang so.
 */

const panelBase =
  'rounded-lg border border-line bg-surface transition-colors duration-300 ease-[var(--ease-smooth)]';

export function Panel({
  children,
  className = '',
  as: Tag = 'div',
}: {
  children: ReactNode;
  className?: string;
  as?: 'div' | 'section' | 'article';
}) {
  return <Tag className={`${panelBase} ${className}`}>{children}</Tag>;
}

export function PanelHeader({
  title,
  description,
  action,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-4 border-b border-line px-5 py-4">
      <div className="min-w-0">
        <h2 className="text-sm font-medium tracking-[-0.01em] text-fg">{title}</h2>
        {description ? <p className="mt-1 text-ui text-dim">{description}</p> : null}
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  );
}

/**
 * The so lieu.
 *
 * `hint` la cho de noi ro con so nghia la gi. Mot con so tien khong kem don vi va
 * khoang thoi gian la mot con so khach khong the doi soat.
 */
export function StatCard({
  label,
  value,
  hint,
  tone = 'default',
}: {
  label: string;
  value: string;
  hint?: string;
  tone?: 'default' | 'accent' | 'warn' | 'danger';
}) {
  const valueTone = {
    default: 'text-fg',
    accent: 'text-accent-light',
    warn: 'text-warn',
    danger: 'text-danger',
  }[tone];

  return (
    <Panel className="p-5">
      <p className="font-mono text-label tracking-[0.14em] text-dim uppercase">{label}</p>
      <p className={`mt-3 font-display text-2xl tracking-[-0.02em] tabular-nums ${valueTone}`}>
        {value}
      </p>
      {hint ? <p className="mt-2 text-ui leading-snug text-dim">{hint}</p> : null}
    </Panel>
  );
}

const badgeTones = {
  neutral: 'border-line bg-surface-hover text-muted',
  accent: 'border-accent/30 bg-accent-soft text-accent-light',
  warn: 'border-warn/30 bg-warn/10 text-warn',
  danger: 'border-danger/30 bg-danger-soft text-danger',
  info: 'border-info/30 bg-info/10 text-info',
} as const;

export type BadgeTone = keyof typeof badgeTones;

export function Badge({
  children,
  tone = 'neutral',
  title,
}: {
  children: ReactNode;
  tone?: BadgeTone;
  title?: string;
}) {
  return (
    <span
      title={title}
      className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 font-mono text-label tracking-[0.06em] whitespace-nowrap ${badgeTones[tone]}`}
    >
      {children}
    </span>
  );
}

/** Trang thai rong. Luon co mot hanh dong hoac mot cau giai thich, khong de trong. */
export function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="px-5 py-16 text-center">
      <p className="text-sm text-muted">{title}</p>
      <p className="mx-auto mt-2 max-w-md text-ui leading-relaxed text-dim">{description}</p>
      {action ? <div className="mt-6 flex justify-center">{action}</div> : null}
    </div>
  );
}

/**
 * Khung xuong khi dang tai.
 *
 * `aria-hidden` va mot vung `role="status"` rieng: doc tung o xuong khong giup gi
 * cho nguoi dung trinh doc man hinh, mot cau "dang tai" thi co.
 */
export function SkeletonRows({ rows = 5, label }: { rows?: number; label: string }) {
  return (
    <>
      <p role="status" className="sr-only">
        {label}
      </p>
      <div aria-hidden className="divide-y divide-line">
        {Array.from({ length: rows }).map((_, i) => (
          <div key={i} className="flex items-center gap-4 px-5 py-4">
            <div className="h-3 w-1/4 animate-pulse rounded-sm bg-white/5" />
            <div className="h-3 w-1/6 animate-pulse rounded-sm bg-white/5" />
            <div className="ml-auto h-3 w-16 animate-pulse rounded-sm bg-white/5" />
          </div>
        ))}
      </div>
    </>
  );
}

/** Thong bao loi kem hanh dong thu lai - loi khong the la duong cung. */
export function ErrorNotice({
  message,
  onRetry,
  retryLabel,
}: {
  message: string;
  onRetry?: () => void;
  retryLabel?: string;
}) {
  return (
    <div
      role="alert"
      className="flex flex-wrap items-center justify-between gap-4 rounded-lg border border-danger/30 bg-danger-soft px-5 py-4"
    >
      <p className="text-ui text-danger">{message}</p>
      {onRetry && retryLabel ? (
        <button
          type="button"
          onClick={onRetry}
          className="rounded-full border border-danger/40 px-4 py-1.5 text-ui text-danger transition-colors hover:bg-danger/10"
        >
          {retryLabel}
        </button>
      ) : null}
    </div>
  );
}

/** Bang co the cuon ngang tren mobile ma khong lam vo bo cuc. */
export function TableScroll({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[42rem] border-collapse text-left">{children}</table>
    </div>
  );
}

export function Th({
  children,
  align = 'left',
}: {
  children: ReactNode;
  align?: 'left' | 'right';
}) {
  return (
    <th
      scope="col"
      className={`border-b border-line px-5 py-3 font-mono text-label font-normal tracking-[0.12em] text-dim uppercase ${
        align === 'right' ? 'text-right' : 'text-left'
      }`}
    >
      {children}
    </th>
  );
}

export function Td({
  children,
  align = 'left',
  className = '',
}: {
  children: ReactNode;
  align?: 'left' | 'right';
  className?: string;
}) {
  return (
    <td
      className={`px-5 py-3.5 text-ui ${align === 'right' ? 'text-right tabular-nums' : ''} ${className}`}
    >
      {children}
    </td>
  );
}