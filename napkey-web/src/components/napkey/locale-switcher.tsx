'use client';

import { useTransition } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { usePathname, useRouter } from '@/i18n/navigation';
import { locales, type Locale } from '@/i18n/routing';

const labels: Record<Locale, string> = { vi: 'VI', en: 'EN' };

export function LocaleSwitcher() {
  const t = useTranslations('nav');
  const locale = useLocale() as Locale;
  const router = useRouter();
  const pathname = usePathname();
  const [isPending, startTransition] = useTransition();

  return (
    <div
      role="group"
      aria-label={t('switchLanguage')}
      className="inline-flex items-center rounded-full border border-line bg-surface-2 p-0.5"
    >
      {locales.map((code) => {
        const isActive = code === locale;
        return (
          <button
            key={code}
            type="button"
            lang={code}
            aria-current={isActive ? 'true' : undefined}
            disabled={isPending}
            onClick={() => {
              if (isActive) return;
              startTransition(() => {
                router.replace(pathname, { locale: code });
              });
            }}
            className={
              'cursor-pointer rounded-full px-3 py-1 font-mono text-label tracking-[0.08em] transition-colors ' +
              'duration-150 ease-[var(--ease-smooth)] disabled:opacity-60 ' +
              (isActive ? 'bg-accent-soft text-accent-light' : 'text-dim hover:text-muted')
            }
          >
            {labels[code]}
          </button>
        );
      })}
    </div>
  );
}
