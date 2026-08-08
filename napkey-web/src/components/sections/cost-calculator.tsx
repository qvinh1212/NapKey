'use client';

import { useId, useMemo, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { MIN_TOPUP_VND, formatVnd, requestCostVnd, requestShapes } from '@/lib/pricing';

const REQUESTS_PER_DAY_STEPS = [10, 25, 50, 100, 200, 400, 800, 1_500, 3_000] as const;
const DAYS_PER_MONTH = 30;

/**
 * Tra loi cau hoi duy nhat khach hoi o trang gia: "moi thang toi tra bao nhieu".
 *
 * Bang gia tinh chi noi mot request ton bao nhieu, roi de nguoi doc tu nhan. Con
 * so thang lai la con so ho dung de quyet dinh, nen tinh luon.
 *
 * Dung `requestCostVnd` va `requestShapes` co san chu khong nhap lai cong thuc:
 * mot ban sao thu hai la mot cho de lech khi don gia doi.
 */
export function CostCalculator() {
  const t = useTranslations('pricing.calculator');
  const locale = useLocale();
  const sliderId = useId();
  const [stepIndex, setStepIndex] = useState(3);
  const [shapeKey, setShapeKey] = useState(requestShapes[1]!.key);

  const requestsPerDay = REQUESTS_PER_DAY_STEPS[stepIndex]!;
  const shape = requestShapes.find((item) => item.key === shapeKey) ?? requestShapes[0]!;

  const { perMonth, perDay, topUpLasts } = useMemo(() => {
    const unit = requestCostVnd(shape.inputTokens, shape.outputTokens);
    const daily = unit * requestsPerDay;
    return {
      perMonth: Math.round(daily * DAYS_PER_MONTH),
      perDay: Math.round(daily),
      topUpLasts: daily > 0 ? Math.floor(MIN_TOPUP_VND / daily) : 0,
    };
  }, [shape, requestsPerDay]);

  const numberFormat = new Intl.NumberFormat(locale === 'en' ? 'en-US' : 'vi-VN');

  return (
    <div className="mt-6 overflow-hidden rounded-xl border border-line bg-surface">
      <div className="grid gap-8 p-6 sm:p-8 lg:grid-cols-[1.05fr_0.95fr] lg:gap-10">
        <div>
          <p className="font-mono text-micro tracking-[0.14em] text-accent uppercase">
            {t('eyebrow')}
          </p>
          <h3 className="mt-3 text-2xl tracking-[-0.02em]">{t('title')}</h3>

          <div className="mt-7">
            <label htmlFor={sliderId} className="flex flex-wrap items-baseline justify-between gap-2">
              <span className="text-prose text-muted">{t('volumeLabel')}</span>
              <span className="font-mono text-lg text-fg tabular-nums">
                {t('volumeValue', { count: numberFormat.format(requestsPerDay) })}
              </span>
            </label>
            <input
              id={sliderId}
              type="range"
              min={0}
              max={REQUESTS_PER_DAY_STEPS.length - 1}
              step={1}
              value={stepIndex}
              onChange={(event) => setStepIndex(Number(event.target.value))}
              aria-valuetext={t('volumeValue', { count: numberFormat.format(requestsPerDay) })}
              className="mt-4 h-11 w-full cursor-pointer accent-[var(--color-accent)]"
            />
          </div>

          <fieldset className="mt-7">
            <legend className="text-prose text-muted">{t('shapeLabel')}</legend>
            <div className="mt-4 flex flex-wrap gap-2">
              {requestShapes.map((item) => {
                const selected = item.key === shape.key;
                return (
                  <button
                    key={item.key}
                    type="button"
                    aria-pressed={selected}
                    onClick={() => setShapeKey(item.key)}
                    className={
                      'min-h-11 rounded-full border px-4 py-2 text-ui transition-colors duration-150 ease-[var(--ease-smooth)] ' +
                      (selected
                        ? 'border-accent/60 bg-accent-soft text-accent-light'
                        : 'border-line text-muted hover:bg-surface-hover hover:text-fg')
                    }
                  >
                    {t(`shapes.${item.key}`)}
                  </button>
                );
              })}
            </div>
          </fieldset>
        </div>

        <div className="rounded-xl border border-accent/30 bg-accent-soft/60 p-6 sm:p-7">
          <p className="font-mono text-micro tracking-[0.14em] text-accent-light uppercase">
            {t('resultLabel')}
          </p>
          <p
            aria-live="polite"
            className="mt-4 font-mono text-4xl leading-none text-fg tabular-nums sm:text-5xl"
          >
            {numberFormat.format(perMonth)}
            <span className="ml-2 align-baseline text-base text-dim">{t('perMonth')}</span>
          </p>
          <dl className="mt-7 space-y-3 border-t border-line/70 pt-5 text-ui">
            <div className="flex items-baseline justify-between gap-4">
              <dt className="text-muted">{t('perDayLabel')}</dt>
              <dd className="font-mono text-fg tabular-nums">{formatVnd(perDay, locale)}</dd>
            </div>
            <div className="flex items-baseline justify-between gap-4">
              <dt className="text-muted">{t('topUpLabel', { amount: formatVnd(MIN_TOPUP_VND, locale) })}</dt>
              <dd className="font-mono text-accent-light tabular-nums">
                {t('topUpValue', { count: numberFormat.format(topUpLasts) })}
              </dd>
            </div>
          </dl>
          <p className="mt-6 text-ui leading-relaxed text-dim">{t('note')}</p>
        </div>
      </div>
    </div>
  );
}
