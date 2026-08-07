'use client';

import { useLocale, useTranslations } from 'next-intl';
import type { UsageRecord } from '@/lib/api/types';
import { count, dateTime, latency, money } from '@/lib/format';
import { Badge } from './ui';

/**
 * Why one request cost what it cost.
 *
 * A customer disputing a charge needs the parts, not the total. This is the row a
 * support conversation is settled with, so every number here comes from the ledger as
 * it was frozen at settlement -- nothing is recomputed from today's price book.
 *
 * The per-request fee gets its own line because on a short request it is most of the
 * bill: 300 VND of a 330 VND charge. A single total makes that look arbitrary, and a
 * price a customer cannot explain is one they assume is wrong.
 */
export function UsageRecordDetail({ record }: { record: UsageRecord }) {
  const t = useTranslations('console.usage.detail');
  const locale = useLocale();

  const tokenRows = [
    { key: 'input', value: record.tokens.input },
    { key: 'output', value: record.tokens.output },
    { key: 'cacheRead', value: record.tokens.cacheRead },
    { key: 'cacheWrite', value: record.tokens.cacheWrite },
  ].filter((row) => row.value > 0);

  return (
    <div className="grid gap-6 border-t border-line bg-black/40 px-5 py-5 lg:grid-cols-2">
      <section>
        <h3 className="font-mono text-label tracking-[0.14em] text-dim uppercase">
          {t('chargeTitle')}
        </h3>
        <dl className="mt-3 space-y-2">
          <div className="flex items-baseline justify-between gap-4 text-ui">
            <dt className="text-muted">{t('tokenCost')}</dt>
            <dd className="font-mono tabular-nums text-fg">{money(record.tokenCost)}</dd>
          </div>
          <div className="flex items-baseline justify-between gap-4 text-ui">
            <dt className="text-muted">{t('requestFee')}</dt>
            <dd className="font-mono tabular-nums text-fg">{money(record.requestFee)}</dd>
          </div>
          <div className="flex items-baseline justify-between gap-4 border-t border-line pt-2 text-ui">
            <dt className="text-fg">{t('total')}</dt>
            <dd className="font-mono text-base tabular-nums text-accent-light">
              {money(record.cost)}
            </dd>
          </div>
        </dl>
        <p className="mt-3 text-ui leading-relaxed text-dim">{t('feeNote')}</p>
      </section>

      <section>
        <h3 className="font-mono text-label tracking-[0.14em] text-dim uppercase">
          {t('tokensTitle')}
        </h3>
        <dl className="mt-3 space-y-2">
          {tokenRows.map((row) => (
            <div key={row.key} className="flex items-baseline justify-between gap-4 text-ui">
              <dt className="text-muted">{t(`tokens.${row.key}`)}</dt>
              <dd className="font-mono tabular-nums text-fg">{count(row.value, locale)}</dd>
            </div>
          ))}
          <div className="flex items-baseline justify-between gap-4 border-t border-line pt-2 text-ui">
            <dt className="text-fg">{t('tokens.total')}</dt>
            <dd className="font-mono tabular-nums text-fg">
              {count(record.tokens.total, locale)}
            </dd>
          </div>
        </dl>
        {record.estimated ? (
          <p className="mt-3 text-ui leading-relaxed text-warn">{t('estimatedNote')}</p>
        ) : null}
      </section>

      <section className="lg:col-span-2">
        <h3 className="font-mono text-label tracking-[0.14em] text-dim uppercase">
          {t('traceTitle')}
        </h3>
        <dl className="mt-3 grid gap-x-8 gap-y-2 sm:grid-cols-2">
          <Trace label={t('trace.requestId')} value={record.requestId} mono />
          <Trace label={t('trace.model')} value={record.model} mono />
          <Trace label={t('trace.time')} value={dateTime(record.createdAt, locale)} />
          <Trace label={t('trace.latency')} value={latency(record.latencyMs, locale)} />
          <Trace
            label={t('trace.key')}
            value={record.keyName || record.keyMasked || t('trace.keyDeleted')}
            mono
          />
          <Trace
            label={t('trace.rate')}
            value={record.rateId ? t('trace.rateValue', { id: record.rateId }) : t('trace.noRate')}
          />
        </dl>
        {record.unpriced ? (
          <p className="mt-3 text-ui leading-relaxed text-warn">{t('unpricedNote')}</p>
        ) : null}
        <p className="mt-3 text-ui leading-relaxed text-dim">{t('disputeNote')}</p>
      </section>
    </div>
  );
}

function Trace({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-4 text-ui">
      <dt className="shrink-0 text-muted">{label}</dt>
      <dd className={`min-w-0 truncate text-right ${mono ? 'font-mono' : ''} text-fg`} title={value}>
        {value}
      </dd>
    </div>
  );
}

/** The badge shown on a collapsed row when a charge needs explaining. */
export function UsageQualityBadge({ record }: { record: UsageRecord }) {
  const t = useTranslations('console.usage');
  if (record.unpriced) return <Badge tone="warn">{t('quality.unpriced')}</Badge>;
  if (record.estimated) return <Badge tone="info">{t('quality.estimated')}</Badge>;
  return null;
}
