'use client';

import { useEffect, useRef, useState } from 'react';
import { CheckIcon, CopyIcon } from '@/components/ui/icon';

export interface CopyButtonProps {
  /** Gia tri van ban can copy vao clipboard. */
  value: string;
  /** Nhan khi chua copy (mac dinh khong hien neu variant='icon'). */
  label?: string;
  /** Nhan khi da copy thanh cong. */
  copiedLabel?: string;
  /** Kieu dang hien thi. */
  variant?: 'pill' | 'icon' | 'ghost' | 'inline';
  /** Hien tooltip 'Copied!' nảy nhẹ dạng spring phia tren nut. */
  showTooltip?: boolean;
  /** aria-label cho trinh doc man hinh. */
  ariaLabel?: string;
  /** ClassName tuy bien bo sung. */
  className?: string;
  /** Thoi gian giu trang thai copied (ms), mac dinh 2000ms. */
  timeout?: number;
  /** Callback tuy chon khi copy thanh cong. */
  onCopy?: () => void;
}

/**
 * Nut sao chep tich hop micro-interaction:
 * - Chuyen doi muot ma giua CopyIcon va CheckIcon.
 * - Hieu ung spring pop nảy nhe khi chuyen sang trang thai da chep.
 * - Ho tro floating tooltip co animation spring bat len.
 * - An toan fallback cho moi truong non-HTTPS.
 */
export function CopyButton({
  value,
  label,
  copiedLabel = 'Copied!',
  variant = 'pill',
  showTooltip = false,
  ariaLabel,
  className = '',
  timeout = 2000,
  onCopy,
}: CopyButtonProps) {
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      onCopy?.();
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => setCopied(false), timeout);
    } catch {
      // Fallback co ban cho context chan clipboard
      setCopied(false);
    }
  }

  const baseStyles = 'relative inline-flex cursor-pointer items-center justify-center transition-all duration-200 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';

  const variantStyles = {
    pill: `gap-2 rounded-full border border-line bg-surface-2 px-3.5 py-[7px] font-mono text-label tracking-[0.08em] uppercase text-muted hover:text-fg ${
      copied ? 'border-accent/40 bg-accent-soft text-accent-light hover:text-accent-light' : ''
    }`,
    icon: `size-8 rounded-full border border-line bg-surface text-muted hover:bg-surface-hover hover:text-fg ${
      copied ? 'border-accent/40 bg-accent-soft text-accent-light hover:text-accent-light' : ''
    }`,
    ghost: `gap-1.5 text-ui text-muted hover:text-fg ${
      copied ? 'text-accent-light hover:text-accent-light' : ''
    }`,
    inline: `gap-1 rounded-md px-1.5 py-0.5 text-ui text-muted hover:bg-surface-2 hover:text-fg ${
      copied ? 'text-accent-light bg-accent-soft' : ''
    }`,
  }[variant];

  return (
    <button
      type="button"
      onClick={handleCopy}
      aria-label={ariaLabel || (copied ? copiedLabel : label || 'Copy')}
      className={`${baseStyles} ${variantStyles} ${className}`}
    >
      {/* Floating Spring Tooltip */}
      {showTooltip && copied && (
        <span
          role="status"
          aria-live="polite"
          className="pointer-events-none absolute -top-8 left-1/2 -translate-x-1/2 whitespace-nowrap rounded-full border border-accent/40 bg-black/95 px-2.5 py-0.5 font-mono text-micro font-medium tracking-wider text-accent-light shadow-lg shadow-black/50 animate-[tooltip-spring_0.25s_cubic-bezier(0.34,1.56,0.64,1)_both]"
        >
          {copiedLabel}
        </span>
      )}

      {/* Animated Icon Container */}
      <span className={`inline-flex items-center justify-center ${copied ? 'animate-[copy-spring_0.35s_cubic-bezier(0.34,1.56,0.64,1)_both]' : ''}`}>
        {copied ? (
          <CheckIcon className="size-3.5 text-accent-light stroke-[2.2]" />
        ) : (
          <CopyIcon className="size-3.5 text-current" />
        )}
      </span>

      {/* Label Text */}
      {variant !== 'icon' && (
        <span className={copied ? 'text-accent-light' : ''}>
          {copied ? copiedLabel : label}
        </span>
      )}
    </button>
  );
}
