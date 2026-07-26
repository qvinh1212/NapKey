'use client';

import { useId, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import type { UsageDayBucket } from '@/lib/api/types';
import { count, dayLabel, money } from '@/lib/format';

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
    <div className="px-5 py-5">
      <div className="mb-3 flex h-5 items-center justify-between text-ui">
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

      {/*
        Cot dung flex thay vi toa do SVG: chieu rong tu chia deu theo so ngay, va
        khong phai tinh lai khi container doi kich thuoc.
      */}
      <div
        aria-hidden
        className="flex h-40 items-end gap-[2px]"
        onMouseLeave={() => setActive(null)}
      >
        {daily.map((day, index) => {
          // Toi thieu 2% de mot ngay co traffic rat nho van thay duoc; mot cot cao
          // 0px khong phan biet duoc voi ngay khong co du lieu.
          const heightPct = Math.max((day.cost.micros / maxMicros) * 100, 2);
          const isActive = active === index;
          return (
            <div
              key={day.day}
              onMouseEnter={() => setActive(index)}
              className="flex h-full flex-1 cursor-default items-end"
            >
              <div
                style={{ height: `${heightPct}%` }}
                className={
                  'w-full rounded-t-sm transition-colors duration-150 ease-[var(--ease-smooth)] ' +
                  (isActive ? 'bg-accent-light' : 'bg-accent/45')
                }
              />
            </div>
          );
        })}
      </div>

      <div aria-hidden className="mt-2 flex justify-between font-mono text-label text-dim">
        <span>{dayLabel(daily[0]!.day, locale)}</span>
        <span>{dayLabel(daily[daily.length - 1]!.day, locale)}</span>
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