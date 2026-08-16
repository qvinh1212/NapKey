'use client';

import { useId, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { formatVnd, requestCostVnd, requestShapes, type RequestShape } from '@/lib/pricing';
import { CheckIcon } from '@/components/ui/icon';

const FIXED_PLAN_VND = 520_000; // $20 USD quy doi ~520.000 VND

const PRESET_REQUESTS = [
  { key: 'light', value: 20 },
  { key: 'moderate', value: 60 },
  { key: 'heavy', value: 150 },
] as const;

const MODEL_OPTIONS = [
  { id: 'claude-sonnet-5', label: 'Claude 3.5 Sonnet' },
  { id: 'claude-opus-4.7', label: 'Claude Opus 4.7' },
  { id: 'claude-opus-5', label: 'Claude Opus 5' },
] as const;

export function PricingCalculator() {
  const t = useTranslations('pricing.calculator');
  const locale = useLocale();
  const sliderId = useId();

  const [requestsPerDay, setRequestsPerDay] = useState(60);
  const [selectedModel, setSelectedModel] = useState<string>('claude-sonnet-5');
  const [workloadKey, setWorkloadKey] = useState<string>('agentStep');

  const shape: RequestShape =
    requestShapes.find((s) => s.key === workloadKey) ?? requestShapes[1]!;

  const requestsPerMonth = requestsPerDay * 30;
  const costPerReq = requestCostVnd(shape.inputTokens, shape.outputTokens, selectedModel);
  const monthlyCost = Math.round(requestsPerMonth * costPerReq);
  const dailyAverage = Math.round(monthlyCost / 30);
  const savings = Math.max(0, FIXED_PLAN_VND - monthlyCost);
  const savingsPercent = Math.round((savings / FIXED_PLAN_VND) * 100);

  return (
    <div className="mt-10 overflow-hidden rounded-2xl border border-line bg-surface p-6 sm:p-8">
      {/* Header */}
      <div className="max-w-2xl">
        <div className="inline-flex items-center gap-2 rounded-full border border-accent/30 bg-accent-soft px-3 py-1 font-mono text-micro uppercase tracking-widest text-accent-light">
          <span className="size-1.5 rounded-full bg-accent" />
          {t('eyebrow')}
        </div>
        <h3 className="mt-3 text-xl font-bold tracking-[-0.02em] text-fg sm:text-2xl">
          {t('title')}
        </h3>
        <p className="mt-2 text-ui text-muted">{t('subtitle')}</p>
      </div>

      {/* Main Grid */}
      <div className="mt-8 grid gap-8 lg:grid-cols-[1.2fr_1fr]">
        {/* Left Column: Interactive Controls */}
        <div className="space-y-6">
          {/* Preset Buttons */}
          <div>
            <span className="block font-mono text-label text-dim uppercase tracking-wider">
              {t('dailyRequests')}
            </span>
            <div className="mt-2.5 flex flex-wrap gap-2">
              {PRESET_REQUESTS.map(({ key, value }) => (
                <button
                  key={key}
                  type="button"
                  onClick={() => setRequestsPerDay(value)}
                  className={`rounded-full border px-3.5 py-1.5 font-mono text-label transition-colors ${
                    requestsPerDay === value
                      ? 'border-accent bg-accent-soft text-accent-light font-semibold'
                      : 'border-line text-muted hover:border-line hover:text-fg'
                  }`}
                >
                  {t(`presets.${key}`)}
                </button>
              ))}
            </div>
          </div>

          {/* Slider */}
          <div>
            <div className="flex items-center justify-between font-mono">
              <label htmlFor={sliderId} className="text-ui text-fg font-medium">
                {t('requestsPerDay', { count: requestsPerDay })}
              </label>
              <span className="text-label text-dim">
                {t('monthlyRequests', { count: requestsPerMonth.toLocaleString() })}
              </span>
            </div>
            <input
              id={sliderId}
              type="range"
              min={10}
              max={300}
              step={5}
              value={requestsPerDay}
              onChange={(e) => setRequestsPerDay(Number(e.target.value))}
              aria-label={t('dailyRequests')}
              className="mt-3 w-full accent-accent cursor-pointer"
            />
            <div className="mt-1 flex justify-between font-mono text-micro text-dim">
              <span>10 req/ngày</span>
              <span>150 req/ngày</span>
              <span>300 req/ngày</span>
            </div>
          </div>

          {/* Model & Workload Selectors */}
          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <label htmlFor="calculator-model-select" className="block font-mono text-label text-dim uppercase tracking-wider">
                {t('modelLabel')}
              </label>
              <select
                id="calculator-model-select"
                value={selectedModel}
                onChange={(e) => setSelectedModel(e.target.value)}
                className="mt-1.5 w-full rounded-lg border border-line bg-surface-hover px-3 py-2 font-mono text-ui text-fg outline-none focus:border-accent"
              >
                {MODEL_OPTIONS.map((m) => (
                  <option key={m.id} value={m.id} className="bg-surface-3 text-fg">
                    {m.label}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label htmlFor="calculator-workload-select" className="block font-mono text-label text-dim uppercase tracking-wider">
                {t('workloadLabel')}
              </label>
              <select
                id="calculator-workload-select"
                value={workloadKey}
                onChange={(e) => setWorkloadKey(e.target.value)}
                className="mt-1.5 w-full rounded-lg border border-line bg-surface-hover px-3 py-2 font-mono text-ui text-fg outline-none focus:border-accent"
              >
                {requestShapes.map((s) => (
                  <option key={s.key} value={s.key} className="bg-surface-3 text-fg">
                    {t(`workloads.${s.key}`)}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </div>

        {/* Right Column: Comparative Results */}
        <div className="flex flex-col justify-between rounded-xl border border-accent bg-accent-soft/30 p-5 sm:p-6 backdrop-blur-sm">
          <div>
            <div className="flex items-center justify-between">
              <span className="font-mono text-label uppercase tracking-wider text-accent-light">
                {t('napkeyCost')}
              </span>
              {savingsPercent > 0 ? (
                <span className="rounded-full border border-accent/40 bg-accent-soft px-2.5 py-0.5 font-mono text-label font-bold text-accent-light">
                  {t('savingsBadge', {
                    percent: savingsPercent,
                    amount: formatVnd(savings, locale),
                  })}
                </span>
              ) : (
                <span className="font-mono text-label text-dim">{t('overageNotice')}</span>
              )}
            </div>

            <div className="mt-3">
              <p className="font-mono text-3xl font-extrabold tracking-tight text-fg sm:text-4xl">
                {formatVnd(monthlyCost, locale)}
                <span className="text-base font-normal text-dim"> / tháng</span>
              </p>
              <p className="mt-1 font-mono text-label text-muted">
                {t('perDayAverage', { amount: formatVnd(dailyAverage, locale) })}
              </p>
            </div>

            {/* Comparison with Fixed Plan */}
            <div className="mt-5 border-t border-line/60 pt-4">
              <div className="flex items-center justify-between font-mono text-ui">
                <span className="text-muted">{t('fixedPlanCost')}:</span>
                <span className="line-through text-dim">{formatVnd(FIXED_PLAN_VND, locale)}</span>
              </div>
              <p className="mt-1 text-label text-dim">{t('fixedPlanDesc')}</p>
            </div>

            {/* Key Value Point */}
            <div className="mt-4 flex items-start gap-2 text-label text-accent-light">
              <CheckIcon className="size-4 shrink-0 stroke-[2.5]" />
              <span>{t('zeroWaste')}</span>
            </div>
          </div>

          <div className="mt-6 pt-2">
            <Link
              href="/signup"
              className="block w-full rounded-lg bg-fg py-2.5 text-center text-ui font-semibold text-bg transition-opacity hover:opacity-90"
            >
              Bắt đầu với 20.000 ₫ trải nghiệm
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}
