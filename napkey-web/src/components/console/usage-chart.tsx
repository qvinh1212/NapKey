'use client';

import { useId, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import type { UsageDayBucket } from '@/lib/api/types';
import { count, dayLabel, money } from '@/lib/format';
import { usageBarPercent } from '@/lib/usage-chart-layout';

/**
 * Bieu do cot chi phi theo ngay.
 *
 * SVG viet tay chu khong dung thu vien chart. Du lieu la mot chuoi mot chieu voi
 * duoi 400 diem; keo them mot thu vien chart vao bundle cho viec nay la doi mot
 * phan dang ke kich thuoc tai ve de lay mot thu 60 dong lam duoc. Neu ve sau can
 * chart phuc tap (nhieu truc, zoom, brush) thi luc do hay doi.
 *
 * Truy cap duoc: `<table>` an di mang toan bo so lieu cho trinh doc man hinh, con
 * SVG duoc `aria-hidden`. Mot bieu do chi ton tai bang mau sac va chieu cao thi
 * nguoi khong nhin thay khong doc duoc gi.
 */

/** Ngay khong co traffic bi backend bo qua, nen chart tu chua khoang trong. */
export function UsageChart({ daily }: { daily: UsageDayBucket[] }) {
  const t = useTranslations('console.usage');
  const locale = useLocale();
  const captionId = useId();
  const [active, setActive] = useState<number | null>(null);

  if (daily.length === 0) {
    return (
      <p className="px-5 py-12 text-center text-ui text-dim">{t('chartEmpty')}</p>
    );
  }

  const maxMicros = Math.max(...daily.map((d) => d.cost.micros), 1);
  const activeDay = active === null ? null : daily[active];

  return (
    <div className="px-4 py-5 sm:px-5">
      <div className="mb-4 flex min-h-5 flex-col gap-1 text-ui sm:mb-3 sm:flex-row sm:items-center sm:justify-between">
        <span className="text-dim">{t('chartLegend')}</span>
        {activeDay ? (
          <span className="tabular-nums text-muted">
            <span className="text-dim">{dayLabel(activeDay.day, locale)}</span>
            {'  '}
            <span className="text-accent-light">{money(activeDay.cost)}</span>
            {'  '}
            <span className="text-dim">
              {t('chartRequests', { count: count(activeDay.requests, locale) })}
            </span>
          </span>
        ) : null}
      </div>

      <div
        aria-hidden
        className="space-y-3"
        onMouseLeave={() => setActive(null)}
      >
        {daily.map((day, index) => {
          const widthPct = usageBarPercent(day.cost.micros, maxMicros);
          const isActive = active === index;
          return (
            <div
              key={day.day}
              onMouseEnter={() => setActive(index)}
              className="grid cursor-default grid-cols-[4.25rem_minmax(0,1fr)] items-center gap-x-3 gap-y-1.5 sm:grid-cols-[5.5rem_minmax(0,1fr)_10rem]"
            >
              <span className="font-mono text-label text-dim">{dayLabel(day.day, locale)}</span>
              <span className="h-2 overflow-hidden rounded-full bg-white/[0.07]">
                <span
                  style={{ width: `${widthPct}%` }}
                  className={'block h-full rounded-full transition-[width,background-color] duration-200 ease-[var(--ease-smooth)] ' +
                    (isActive ? 'bg-accent-light shadow-[0_0_14px_rgba(52,211,153,0.3)]' : 'bg-accent/65')}
                />
              </span>
              <span className="col-span-2 text-right font-mono text-label tabular-nums text-muted sm:col-span-1">
                {money(day.cost)}
                <span className="ml-2 hidden text-dim sm:inline">· {count(day.requests, locale)} req</span>
              </span>
            </div>
          );
        })}
      </div>

      {/*
        Ban day du cho trinh doc man hinh. `sr-only` chu khong `display:none` -
        display:none thi trinh doc man hinh cung khong thay.
      */}
      <table className="sr-only" aria-describedby={captionId}>
        <caption id={captionId}>{t('chartTableCaption')}</caption>
        <thead>
          <tr>
            <th scope="col">{t('colDay')}</th>
            <th scope="col">{t('colRequests')}</th>
            <th scope="col">{t('colTokens')}</th>
            <th scope="col">{t('colCost')}</th>
          </tr>
        </thead>
        <tbody>
          {daily.map((day) => (
            <tr key={day.day}>
              <th scope="row">{dayLabel(day.day, locale)}</th>
              <td>{count(day.requests, locale)}</td>
              <td>{count(day.tokens.total, locale)}</td>
              <td>{money(day.cost)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
