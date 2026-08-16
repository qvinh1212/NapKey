'use client';

import { useId, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import type { UsageDayBucket } from '@/lib/api/types';
import { count, dayLabel, money } from '@/lib/format';
import { usageBarPercent } from '@/lib/usage-chart-layout';

/**
 * Bieu do cot chi phi va phan bo token theo ngay.
 *
 * SVG & CSS thuan khong dung thu vien nang bundle.
 * Ho tro tooltip tuong tac chi tiet:
 * - Phan tach ro ty le giua Prompt Tokens (Input) va Completion Tokens (Output).
 * - Chi phi thuc te va so request tren tung moc thoi gian.
 * - Mini proportional distribution bar voi animation spring.
 *
 * Truy cap duoc: `<table>` an di mang toan bo so lieu cho trinh doc man hinh.
 */

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
      {/* Header & Legend */}
      <div className="mb-4 flex flex-col gap-2 border-b border-line pb-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap items-center gap-3 text-label">
          <span className="font-medium text-dim">{t('chartLegend')}</span>
          <span className="inline-flex items-center gap-1.5 text-muted">
            <span className="size-2 rounded-full bg-accent" />
            <span>{t('colInput')} (Prompt)</span>
          </span>
          <span className="inline-flex items-center gap-1.5 text-muted">
            <span className="size-2 rounded-full bg-info" />
            <span>{t('colOutput')} (Completion)</span>
          </span>
          <span className="inline-flex items-center gap-1.5 text-dim">
            <span className="size-2 rounded-full bg-zinc-400" />
            <span>{t('colCacheRead')}</span>
          </span>
        </div>

        {activeDay ? (
          <span className="font-mono text-label tabular-nums text-muted">
            <span className="text-dim">{dayLabel(activeDay.day, locale)}</span>
            {' · '}
            <span className="font-medium text-accent-light">{money(activeDay.cost)}</span>
            {' · '}
            <span className="text-dim">
              {t('chartRequests', { count: count(activeDay.requests, locale) })}
            </span>
          </span>
        ) : null}
      </div>

      {/* Chart Bars List */}
      <div
        aria-hidden
        className="space-y-3"
        onMouseLeave={() => setActive(null)}
      >
        {daily.map((day, index) => {
          const widthPct = usageBarPercent(day.cost.micros, maxMicros);
          const isActive = active === index;

          const inputTokens = day.tokens.input;
          const outputTokens = day.tokens.output;
          const cacheTokens = day.tokens.cacheRead;
          const totalTokens = day.tokens.total || (inputTokens + outputTokens + cacheTokens);

          const inputRatio = totalTokens > 0 ? (inputTokens / totalTokens) * 100 : 0;
          const outputRatio = totalTokens > 0 ? (outputTokens / totalTokens) * 100 : 0;
          const cacheRatio = totalTokens > 0 ? (cacheTokens / totalTokens) * 100 : 0;

          return (
            <div
              key={day.day}
              onMouseEnter={() => setActive(index)}
              className="relative"
            >
              <div className="grid cursor-default grid-cols-[4.25rem_minmax(0,1fr)] items-center gap-x-3 gap-y-1.5 sm:grid-cols-[5.5rem_minmax(0,1fr)_10rem]">
                <span className="font-mono text-label text-dim">{dayLabel(day.day, locale)}</span>
                
                {/* Horizontal Segmented Bar */}
                <span className="relative flex h-2.5 overflow-hidden rounded-full bg-white/[0.07]">
                  <span
                    style={{ width: `${widthPct}%` }}
                    className={`flex h-full overflow-hidden rounded-full transition-all duration-200 ease-[var(--ease-smooth)] ${
                      isActive ? 'shadow-[0_0_16px_rgba(52,211,153,0.35)] ring-1 ring-white/20' : ''
                    }`}
                  >
                    {totalTokens > 0 ? (
                      <>
                        <span
                          style={{ width: `${inputRatio}%` }}
                          className="h-full bg-accent transition-colors"
                        />
                        <span
                          style={{ width: `${outputRatio}%` }}
                          className="h-full bg-info transition-colors"
                        />
                        {cacheRatio > 0 ? (
                          <span
                            style={{ width: `${cacheRatio}%` }}
                            className="h-full bg-zinc-400 transition-colors"
                          />
                        ) : null}
                      </>
                    ) : (
                      <span className="h-full w-full bg-accent/65" />
                    )}
                  </span>
                </span>

                <span className="col-span-2 text-right font-mono text-label tabular-nums text-muted sm:col-span-1">
                  {money(day.cost)}
                  <span className="ml-2 hidden text-dim sm:inline">· {count(day.requests, locale)} req</span>
                </span>
              </div>

              {/* Interactive Tooltip Card */}
              {isActive ? (
                <div
                  role="tooltip"
                  className="mt-2 overflow-hidden rounded-lg border border-line bg-black/95 p-3.5 shadow-2xl backdrop-blur-md animate-[tooltip-spring_0.25s_cubic-bezier(0.34,1.56,0.64,1)_both]"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2 border-b border-line pb-2">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-ui font-semibold text-fg">
                        {dayLabel(day.day, locale)}
                      </span>
                      <span className="rounded bg-white/10 px-1.5 py-0.5 font-mono text-micro text-dim">
                        {count(day.requests, locale)} req
                      </span>
                    </div>
                    <div className="font-mono text-ui font-semibold text-accent-light">
                      {money(day.cost)}
                    </div>
                  </div>

                  {/* Token breakdown details */}
                  <div className="mt-2.5 grid grid-cols-2 gap-3 text-label sm:grid-cols-3">
                    <div className="space-y-1">
                      <div className="flex items-center gap-1.5 text-muted">
                        <span className="size-2 rounded-full bg-accent shrink-0" />
                        <span>{t('colInput')} (Prompt)</span>
                      </div>
                      <div className="font-mono text-fg">
                        {count(inputTokens, locale)}{' '}
                        <span className="text-micro text-accent-light">
                          ({inputRatio.toFixed(1)}%)
                        </span>
                      </div>
                    </div>

                    <div className="space-y-1">
                      <div className="flex items-center gap-1.5 text-muted">
                        <span className="size-2 rounded-full bg-info shrink-0" />
                        <span>{t('colOutput')} (Completion)</span>
                      </div>
                      <div className="font-mono text-fg">
                        {count(outputTokens, locale)}{' '}
                        <span className="text-micro text-info">
                          ({outputRatio.toFixed(1)}%)
                        </span>
                      </div>
                    </div>

                    {cacheTokens > 0 ? (
                      <div className="col-span-2 space-y-1 sm:col-span-1">
                        <div className="flex items-center gap-1.5 text-muted">
                          <span className="size-2 rounded-full bg-zinc-400 shrink-0" />
                          <span>{t('colCacheRead')}</span>
                        </div>
                        <div className="font-mono text-fg">
                          {count(cacheTokens, locale)}{' '}
                          <span className="text-micro text-dim">
                            ({cacheRatio.toFixed(1)}%)
                          </span>
                        </div>
                      </div>
                    ) : null}
                  </div>

                  {/* Token distribution mini progress bar */}
                  {totalTokens > 0 ? (
                    <div className="mt-3 border-t border-line/60 pt-2.5">
                      <div className="mb-1 flex justify-between font-mono text-micro text-dim">
                        <span>{t('colTokens')}: {count(totalTokens, locale)}</span>
                        <span>{inputRatio.toFixed(0)}% In / {outputRatio.toFixed(0)}% Out</span>
                      </div>
                      <div className="flex h-1.5 w-full overflow-hidden rounded-full bg-white/10">
                        <div style={{ width: `${inputRatio}%` }} className="bg-accent" />
                        <div style={{ width: `${outputRatio}%` }} className="bg-info" />
                        {cacheRatio > 0 ? <div style={{ width: `${cacheRatio}%` }} className="bg-zinc-400" /> : null}
                      </div>
                    </div>
                  ) : null}
                </div>
              ) : null}
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
