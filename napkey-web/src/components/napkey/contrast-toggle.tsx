'use client';

import { useSyncExternalStore } from 'react';
import { useTranslations } from 'next-intl';
import { ContrastIcon } from '@/components/ui/icon';

export type ContrastMode = 'oled' | 'high';

const STORAGE_KEY = 'napkey-contrast';

function getSnapshot(): ContrastMode {
  if (typeof window === 'undefined') return 'oled';
  const saved = window.localStorage.getItem(STORAGE_KEY);
  if (saved === 'high' || saved === 'oled') return saved;
  if (window.matchMedia('(prefers-contrast: more)').matches) return 'high';
  return 'oled';
}

function getServerSnapshot(): ContrastMode {
  return 'oled';
}

function subscribe(callback: () => void) {
  window.addEventListener('storage', callback);
  window.addEventListener('contrastchange', callback);
  const media = window.matchMedia('(prefers-contrast: more)');
  media.addEventListener('change', callback);
  return () => {
    window.removeEventListener('storage', callback);
    window.removeEventListener('contrastchange', callback);
    media.removeEventListener('change', callback);
  };
}

export function ContrastToggle({ className = '' }: { className?: string }) {
  const t = useTranslations('nav');
  const contrast = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);

  function toggle() {
    const next: ContrastMode = contrast === 'oled' ? 'high' : 'oled';
    document.documentElement.setAttribute('data-contrast', next);
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
      window.dispatchEvent(new Event('contrastchange'));
    } catch {
      // Storage unavailable
    }
  }

  const isHigh = contrast === 'high';

  return (
    <button
      type="button"
      onClick={toggle}
      title={isHigh ? t('contrastHigh') : t('contrastOled')}
      aria-label={t('contrastMode')}
      aria-pressed={isHigh}
      className={`group inline-flex cursor-pointer items-center gap-1.5 rounded-full border border-line bg-surface-2 px-2.5 py-1 font-mono text-label transition-colors duration-150 ease-[var(--ease-smooth)] hover:bg-surface-2 hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${
        isHigh ? 'border-accent/40 bg-accent-soft text-accent-light' : 'text-dim hover:text-muted'
      } ${className}`}
    >
      <ContrastIcon className={`size-3.5 transition-transform duration-200 ${isHigh ? 'rotate-180 text-accent-light' : 'text-current'}`} />
      <span className="tracking-wider uppercase">
        {isHigh ? 'HIGH' : 'OLED'}
      </span>
    </button>
  );
}
