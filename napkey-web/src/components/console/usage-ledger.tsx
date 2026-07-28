'use client';

import { useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { api, ApiError } from '@/lib/api/client';
import type { KeyListResponse, UsageDetailResponse, UsageRecordsResponse } from '@/lib/api/types';
import { billingRange, compact, count, creditAmount, dateTime, latency, money } from '@/lib/format';
import { usagePageQueries } from '@/lib/usage-query';
import { UsageChart } from './usage-chart';
import {
  Badge,
  EmptyState,
  ErrorNotice,
  Panel,
  PanelHeader,
  SkeletonRows,
  StatCard,
  TableScroll,
  Td,
  Th,
} from './ui';

/**
 * So usage tung request.
 *
 * Day la trang lam cho mot khoan tien tra loi duoc: moi request mot dong, kem token
 * tach theo loai, chi phi, va co cho biet output token la DO DUOC hay UOC LUONG.
 * Neu chi hien mot con so tong thi khach khong co cach nao doi soat.
 */

const PAGE_SIZE = 50;

/** Cac khoang thoi gian dat san. Nguoi dung it khi muon nhap ngay bang tay. */
const presets = [7, 30, 90] as const;
type Preset = (typeof presets)[number];

type State =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; records: UsageRecordsResponse; detail: UsageDetailResponse };

export function UsageLedger() {
  const t = useTranslations('console.usage');
  const locale = useLocale();

  const [days, setDays] = useState<Preset>(30);
  const [keyId, setKeyId] = useState('');
  const [offset, setOffset] = useState(0);
  const [state, setState] = useState<State>({ status: 'loading' });
  const [keys, setKeys] = useState<KeyListResponse['keys']>([]);

  // Tang len de yeu cau doc lai cung mot bo loc, dung cho nut "thu lai".
  const [reloadToken, setReloadToken] = useState(0);

  // Danh sach key chi de lam bo loc, nen loi o day khong duoc lam vo ca trang.
  useEffect(() => {
    const controller = new AbortController();

    async function run() {
      try {
        const data = await api.get<KeyListResponse>('/v1/keys', controller.signal);
        setKeys(data.keys);
      } catch {
        setKeys([]);
      }
    }

    void run();
    return () => controller.abort();
  }, []);

  useEffect(() => {
    const controller = new AbortController();

    async function run() {
      setState({ status: 'loading' });
      try {
        const queries = usagePageQueries({
          ...billingRange(days),
          keyId: keyId || undefined,
          limit: PAGE_SIZE,
          offset,
        });
        const [records, detail] = await Promise.all([
          api.get<UsageRecordsResponse>(queries.records, controller.signal),
          api.get<UsageDetailResponse>(queries.detail, controller.signal),
        ]);
        setState({ status: 'ready', records, detail });
      } catch (error) {
        // Doi bo loc nhanh se huy request cu. Do khong phai loi de bao cho nguoi dung.
        if (controller.signal.aborted) return;
        const message = error instanceof ApiError ? error.message : t('loadFailed');
        setState({ status: 'error', message });
      }
    }

    void run();
    return () => controller.abort();
  }, [days, keyId, offset, t, reloadToken]);

  const total = state.status === 'ready' ? state.records.total : 0;
  const hasPrev = offset > 0;
  const hasNext = offset + PAGE_SIZE < total;

  return (
    <div className="flex flex-col gap-6">
      {state.status === 'ready' ? (
        <>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <StatCard
              label={t('stats.credits')}
              value={creditAmount(state.detail.totals.credits, locale)}
              hint={t('stats.rangeHint', { days })}
              tone="accent"
            />
            <StatCard
              label={t('stats.requests')}
              value={count(state.detail.totals.requests, locale)}
              hint={t('stats.errorHint', {
                count: count(state.detail.totals.errorRequests, locale),
              })}
            />
            <StatCard
              label={t('stats.tokens')}
              value={compact(state.detail.totals.tokens.total, locale)}
              hint={t('stats.tokenHint', {
                input: compact(state.detail.totals.tokens.input, locale),
                output: compact(state.detail.totals.tokens.output, locale),
              })}
            />
            <StatCard
              label={t('stats.quality')}
              value={count(state.detail.totals.estimatedRequests, locale)}
              hint={t('stats.qualityHint', {
                unpriced: count(state.detail.totals.unpricedRequests, locale),
              })}
              tone={state.detail.totals.unpricedRequests > 0 ? 'warn' : 'default'}
            />
          </div>

          <Panel as="section">
            <PanelHeader title={t('chartTitle')} description={t('chartDescription')} />
            <UsageChart daily={state.detail.daily} />
          </Panel>
        </>
      ) : null}

      <Panel as="section">
        <PanelHeader
          title={t('title')}
          description={t('description')}
          action={
            <div className="flex flex-wrap items-center gap-2">
              <div
                role="group"
                aria-label={t('rangeLabel')}
                className="inline-flex items-center rounded-full border border-line bg-surface-hover p-0.5"
              >
                {presets.map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-current={value === days ? 'true' : undefined}
                    onClick={() => {
                      setDays(value);
                      // Doi khoang thoi gian phai ve trang dau: giu offset cu se
                      // hien mot trang trong khi khoang moi it du lieu hon.
                      setOffset(0);
                    }}
                    className={
                      'rounded-full px-3 py-1 font-mono text-label transition-colors duration-150 ' +
                      (value === days ? 'bg-white/10 text-fg' : 'text-dim hover:text-muted')
                    }
                  >
                    {t('rangeDays', { days: value })}
                  </button>
                ))}
              </div>

              <label className="sr-only" htmlFor="usage-key-filter">
                {t('filterByKey')}
              </label>
              <select
                id="usage-key-filter"
                value={keyId}
                onChange={(event) => {
                  setKeyId(event.target.value);
                  setOffset(0);
                }}
                className="rounded-full border border-line bg-surface-hover px-3 py-1.5 text-ui text-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              >
                <option value="">{t('allKeys')}</option>
                {keys.map((key) => (
                  <option key={key.id} value={key.id}>
                    {key.name || key.keyMasked}
                  </option>
                ))}
              </select>
            </div>
          }
        />

        {state.status === 'loading' ? <SkeletonRows rows={6} label={t('loading')} /> : null}

        {state.status === 'error' ? (
          <div className="p-5">
            <ErrorNotice
              message={state.message}
              onRetry={() => setReloadToken((v) => v + 1)}
              retryLabel={t('retry')}
            />
          </div>
        ) : null}

        {state.status === 'ready' && state.records.records.length === 0 ? (
          <EmptyState title={t('emptyTitle')} description={t('emptyDescription')} />
        ) : null}

        {state.status === 'ready' && state.records.records.length > 0 ? (
          <>
            <TableScroll>
              <thead>
                <tr>
                  <Th>{t('colTime')}</Th>
                  <Th>{t('colModel')}</Th>
                  <Th>{t('colKey')}</Th>
                  <Th align="right">{t('colInput')}</Th>
                  <Th align="right">{t('colOutput')}</Th>
                  <Th align="right">{t('colCacheRead')}</Th>
                  <Th align="right">{t('colLatency')}</Th>
                  <Th align="right">{t('colCredits')}</Th>
                  <Th align="right">{t('colCost')}</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {state.records.records.map((record) => (
                  <tr key={record.id} className="transition-colors hover:bg-surface-hover">
                    <Td className="whitespace-nowrap text-dim">
                      {dateTime(record.createdAt, locale)}
                    </Td>
                    <Td className="font-mono text-muted">
                      <span className="flex flex-wrap items-center gap-2">
                        {record.model}
                        {record.status !== 'success' ? (
                          <Badge tone={record.status === 'error' ? 'danger' : 'neutral'}>
                            {t(`status.${record.status}`)}
                          </Badge>
                        ) : null}
                        {/* Cho khach biet con so nay la uoc luong, khong phai do duoc. */}
                        {record.estimated ? (
                          <Badge tone="info" title={t('estimatedTooltip')}>
                            {t('estimatedShort')}
                          </Badge>
                        ) : null}
                        {record.unpriced ? (
                          <Badge tone="warn" title={t('unpricedTooltip')}>
                            {t('unpricedShort')}
                          </Badge>
                        ) : null}
                      </span>
                    </Td>
                    <Td className="text-dim">
                      {/* Vang khi key da bi xoa: so usage song lau hon key. */}
                      {record.keyMasked ? (
                        <span className="font-mono" title={record.keyName}>
                          {record.keyMasked}
                        </span>
                      ) : (
                        <span className="text-dim">{t('keyDeleted')}</span>
                      )}
                    </Td>
                    <Td align="right">{count(record.tokens.input, locale)}</Td>
                    <Td align="right">{count(record.tokens.output, locale)}</Td>
                    <Td align="right" className="text-dim">
                      {count(record.tokens.cacheRead, locale)}
                    </Td>
                    <Td align="right" className="text-dim">
                      {latency(record.latencyMs, locale)}
                    </Td>
                    <Td align="right" className="font-mono text-fg">
                      {creditAmount(record.credits, locale)}
                    </Td>
                    <Td align="right" className="text-accent-light">
                      {money(record.cost)}
                    </Td>
                  </tr>
                ))}
              </tbody>
            </TableScroll>

            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line px-5 py-4">
              <p className="text-ui text-dim">
                {t('pageInfo', {
                  from: count(offset + 1, locale),
                  to: count(Math.min(offset + PAGE_SIZE, total), locale),
                  total: count(total, locale),
                })}
              </p>
              <div className="flex gap-2">
                <button
                  type="button"
                  disabled={!hasPrev}
                  onClick={() => setOffset((v) => Math.max(v - PAGE_SIZE, 0))}
                  className="rounded-full border border-line bg-surface-hover px-4 py-1.5 text-ui text-muted transition-colors hover:bg-white/10 hover:text-fg disabled:pointer-events-none disabled:opacity-40"
                >
                  {t('prev')}
                </button>
                <button
                  type="button"
                  disabled={!hasNext}
                  onClick={() => setOffset((v) => v + PAGE_SIZE)}
                  className="rounded-full border border-line bg-surface-hover px-4 py-1.5 text-ui text-muted transition-colors hover:bg-white/10 hover:text-fg disabled:pointer-events-none disabled:opacity-40"
                >
                  {t('next')}
                </button>
              </div>
            </div>
          </>
        ) : null}
      </Panel>
    </div>
  );
}
