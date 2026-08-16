'use client';

import { Fragment, useEffect, useMemo, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { api, ApiError } from '@/lib/api/client';
import type { KeyListResponse, UsageDetailResponse, UsageRecordsResponse } from '@/lib/api/types';
import { billingRange, compact, count, dateTime, latency, money } from '@/lib/format';
import { usagePageQueries } from '@/lib/usage-query';
import { exportUsageCsv } from '@/lib/export-csv';
import { UsageChart } from './usage-chart';
import { usageRecordView } from '@/lib/usage-record-view';
import { UsageRecordDetail, UsageQualityBadge } from './usage-record-detail';
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
  const [modelFilter, setModelFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState<'all' | 'success' | 'error'>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [offset, setOffset] = useState(0);
  const [expandedId, setExpandedId] = useState<number | null>(null);
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

  const availableModels = useMemo(() => {
    if (state.status !== 'ready') return [];
    const set = new Set<string>();
    state.records.records.forEach((r) => {
      if (r.model) set.add(r.model);
    });
    return Array.from(set).sort();
  }, [state]);

  const filteredRecords = useMemo(() => {
    if (state.status !== 'ready') return [];
    return state.records.records.filter((record) => {
      if (modelFilter && record.model.toLowerCase() !== modelFilter.toLowerCase()) return false;
      if (statusFilter !== 'all' && record.status !== statusFilter) return false;
      if (searchQuery.trim()) {
        const q = searchQuery.trim().toLowerCase();
        const matchesId = (record.requestId || '').toLowerCase().includes(q) || String(record.id).includes(q);
        const matchesModel = record.model.toLowerCase().includes(q);
        const matchesKey = (record.keyName || '').toLowerCase().includes(q) || (record.keyMasked || '').toLowerCase().includes(q);
        if (!matchesId && !matchesModel && !matchesKey) return false;
      }
      return true;
    });
  }, [state, modelFilter, statusFilter, searchQuery]);

  const hasActiveAdvancedFilters = Boolean(modelFilter || statusFilter !== 'all' || searchQuery.trim());

  function handleExport() {
    if (state.status !== 'ready') return;
    const exportData = filteredRecords.length > 0 ? filteredRecords : state.records.records;
    const filename = `napkey-usage-${days}d-${new Date().toISOString().slice(0, 10)}.csv`;
    exportUsageCsv(exportData, filename);
  }

  function handleClearFilters() {
    setModelFilter('');
    setStatusFilter('all');
    setSearchQuery('');
  }

  return (
    <div className="flex flex-col gap-6">
      {state.status === 'ready' ? (
        <>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <StatCard
              label={t('stats.spend')}
              value={money(state.detail.totals.cost)}
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
                      setOffset(0);
                      setExpandedId(null);
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
                  setExpandedId(null);
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

              {/* Export CSV Button */}
              {state.status === 'ready' && state.records.records.length > 0 ? (
                <button
                  type="button"
                  onClick={handleExport}
                  className="inline-flex items-center gap-1.5 rounded-full border border-accent/40 bg-accent-soft px-3.5 py-1.5 font-mono text-ui text-accent-light hover:bg-accent/20 transition-colors"
                >
                  <svg className="size-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                    <polyline points="7 10 12 15 17 10" />
                    <line x1="12" y1="15" x2="12" y2="3" />
                  </svg>
                  <span>{t('exportCsv')}</span>
                </button>
              ) : null}
            </div>
          }
        />

        {/* Advanced Filters Toolbar */}
        {state.status === 'ready' && state.records.records.length > 0 ? (
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line bg-surface-hover/50 px-5 py-3">
            <div className="flex flex-wrap items-center gap-2.5">
              {/* Filter by Model */}
              <select
                value={modelFilter}
                onChange={(e) => setModelFilter(e.target.value)}
                className="rounded-lg border border-line bg-surface px-2.5 py-1 font-mono text-label text-muted focus:border-accent"
              >
                <option value="">{t('allModels')}</option>
                {availableModels.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>

              {/* Filter by Status */}
              <select
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value as 'all' | 'success' | 'error')}
                className="rounded-lg border border-line bg-surface px-2.5 py-1 font-mono text-label text-muted focus:border-accent"
              >
                <option value="all">{t('allStatuses')}</option>
                <option value="success">{t('statusSuccess')}</option>
                <option value="error">{t('statusError')}</option>
              </select>

              {/* Search query */}
              <div className="relative">
                <input
                  type="text"
                  placeholder={t('searchPlaceholder')}
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="rounded-lg border border-line bg-surface pl-2.5 pr-7 py-1 font-mono text-label text-fg placeholder:text-dim focus:border-accent outline-none w-48 sm:w-60"
                />
                {searchQuery ? (
                  <button
                    type="button"
                    onClick={() => setSearchQuery('')}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-dim hover:text-fg text-micro"
                  >
                    ✕
                  </button>
                ) : null}
              </div>

              {hasActiveAdvancedFilters ? (
                <button
                  type="button"
                  onClick={handleClearFilters}
                  className="text-micro text-accent-light hover:underline font-mono"
                >
                  {t('clearFilters')}
                </button>
              ) : null}
            </div>

            <div className="font-mono text-micro text-dim">
              {filteredRecords.length} / {state.records.records.length} records
            </div>
          </div>
        ) : null}

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

        {state.status === 'ready' && state.records.records.length > 0 && filteredRecords.length === 0 ? (
          <div className="p-8 text-center">
            <p className="text-ui text-muted">{t('noFilteredResults')}</p>
            <button
              type="button"
              onClick={handleClearFilters}
              className="mt-3 inline-block rounded-full border border-line px-4 py-1.5 text-label text-accent-light hover:bg-surface-hover"
            >
              {t('clearFilters')}
            </button>
          </div>
        ) : null}

        {state.status === 'ready' && filteredRecords.length > 0 ? (
          <>
            <TableScroll>
              <thead>
                <tr>
                  <Th>{t('colTime')}</Th>
                  <Th>{t('colModel')}</Th>
                  <Th>{t('colType')}</Th>
                  <Th>{t('colStatus')}</Th>
                  <Th align="right">{t('colInput')}</Th>
                  <Th align="right">{t('colOutput')}</Th>
                  <Th align="right">{t('colCache')}</Th>
                  <Th align="right">{t('colCost')}</Th>
                  <Th align="right">{t('colLatency')}</Th>
                  <Th align="right">{t('colDetail')}</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {filteredRecords.map((record) => {
                  const view = usageRecordView(record);
                  const open = expandedId === record.id;
                  return (
                    <Fragment key={record.id}>
                      <tr className="transition-colors hover:bg-surface-hover">
                        <Td className="whitespace-nowrap font-mono text-label text-dim">
                          {dateTime(record.createdAt, locale)}
                        </Td>
                        <Td className="font-mono text-muted">
                          <span className="whitespace-nowrap">{record.model}</span>
                        </Td>
                        <Td>
                          <div className="flex flex-wrap items-center gap-1.5">
                            <Badge tone="info">{view.type}</Badge>
                            <UsageQualityBadge record={record} />
                          </div>
                        </Td>
                        <Td><Badge tone={view.statusTone}>{t(`status.${view.status}`)}</Badge></Td>
                        <Td align="right" className="font-mono text-dim tabular-nums">
                          {compact(record.tokens.input, locale)}
                        </Td>
                        <Td align="right" className="font-mono text-dim tabular-nums">
                          {compact(record.tokens.output, locale)}
                        </Td>
                        <Td align="right" className="font-mono text-dim tabular-nums">
                          {compact(record.tokens.cacheRead, locale)}
                        </Td>
                        <Td align="right" className="font-mono font-medium text-fg tabular-nums">
                          {money(record.cost)}
                        </Td>
                        <Td align="right" className="font-mono text-dim tabular-nums">
                          {latency(record.latencyMs, locale)}
                        </Td>
                        <Td align="right">
                          <button
                            type="button"
                            onClick={() => setExpandedId(open ? null : record.id)}
                            aria-expanded={open}
                            aria-controls={`usage-record-${record.id}`}
                            className="rounded-full border border-line px-3 py-1 font-mono text-label text-muted transition-colors hover:border-line hover:text-fg"
                          >
                            {open ? t('hideDetail') : t('showDetail')}
                          </button>
                        </Td>
                      </tr>

                      {open ? (
                        <tr id={`usage-record-${record.id}`}>
                          <td colSpan={10} className="bg-bg/40 p-0">
                            <UsageRecordDetail record={record} />
                          </td>
                        </tr>
                      ) : null}
                    </Fragment>
                  );
                })}
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
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  disabled={!hasPrev}
                  onClick={() => {
                    setOffset((v) => Math.max(0, v - PAGE_SIZE));
                    setExpandedId(null);
                  }}
                  className="rounded-full border border-line px-4 py-1.5 text-ui text-muted transition-colors hover:bg-surface-hover hover:text-fg disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {t('prev')}
                </button>
                <button
                  type="button"
                  disabled={!hasNext}
                  onClick={() => {
                    setOffset((v) => v + PAGE_SIZE);
                    setExpandedId(null);
                  }}
                  className="rounded-full border border-line px-4 py-1.5 text-ui text-muted transition-colors hover:bg-surface-hover hover:text-fg disabled:cursor-not-allowed disabled:opacity-40"
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
