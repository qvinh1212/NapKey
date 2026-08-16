'use client';

import { useMemo, useState, useEffect } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { api, ApiError } from '@/lib/api/client';
import type { ApiKey, CreateKeyResponse, KeyListResponse, KeySyncState } from '@/lib/api/types';
import { compact, dateTime } from '@/lib/format';
import { useSession } from './session-provider';
import { KeyOnboarding } from './key-onboarding';
import { KeyConfigModal } from './key-config-modal';
import { Badge, EmptyState, ErrorNotice, Panel, PanelHeader, SkeletonRows, StatCard, TableScroll, Td, Th, type BadgeTone } from './ui';

const syncTone: Record<KeySyncState, BadgeTone> = {
  pending: 'info',
  synced: 'accent',
  failed: 'danger',
  delete_pending: 'warn',
};

type ListState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; keys: ApiKey[] };

export function KeysManager() {
  const t = useTranslations('console.keys');
  const locale = useLocale();
  const session = useSession();
  const canCreate = session.status === 'authenticated' && session.user.emailVerified;

  const [state, setState] = useState<ListState>({ status: 'loading' });
  const [reloadToken, setReloadToken] = useState(0);
  const [name, setName] = useState('');
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [revealed, setRevealed] = useState<CreateKeyResponse | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [configKey, setConfigKey] = useState<ApiKey | null>(null);

  useEffect(() => {
    const controller = new AbortController();

    async function run() {
      try {
        const data = await api.get<KeyListResponse>('/v1/keys', controller.signal);
        setState({ status: 'ready', keys: data.keys });
      } catch (error) {
        if (controller.signal.aborted) return;
        const message = error instanceof ApiError ? error.message : t('loadFailed');
        setState({ status: 'error', message });
      }
    }

    void run();
    return () => controller.abort();
  }, [t, reloadToken]);

  function reload() {
    setReloadToken((v) => v + 1);
  }

  async function createKey(event: React.FormEvent) {
    event.preventDefault();
    setCreating(true);
    setCreateError(null);
    try {
      const created = await api.post<CreateKeyResponse>('/v1/keys', { name: name.trim() });
      setRevealed(created);
      setName('');
      reload();
    } catch (error) {
      setCreateError(error instanceof ApiError ? error.message : t('createFailed'));
    } finally {
      setCreating(false);
    }
  }

  async function toggleEnabled(key: ApiKey) {
    setBusyId(key.id);
    try {
      await api.patch(`/v1/keys/${key.id}`, { enabled: !key.enabled });
      reload();
    } catch (error) {
      setState({
        status: 'error',
        message: error instanceof ApiError ? error.message : t('updateFailed'),
      });
    } finally {
      setBusyId(null);
    }
  }

  async function revokeKey(key: ApiKey) {
    if (!window.confirm(t('revokeConfirm', { name: key.name || key.keyMasked }))) return;
    setBusyId(key.id);
    try {
      await api.delete(`/v1/keys/${key.id}`);
      reload();
    } catch (error) {
      setState({
        status: 'error',
        message: error instanceof ApiError ? error.message : t('revokeFailed'),
      });
    } finally {
      setBusyId(null);
    }
  }

  const totals = useMemo(() => {
    if (state.status !== 'ready') return { totalKeys: 0, activeKeys: 0, totalRequests: 0, totalTokens: 0 };
    let activeKeys = 0;
    let totalRequests = 0;
    let totalTokens = 0;

    state.keys.forEach((k) => {
      if (k.status === 'active' && k.enabled) activeKeys++;
      totalRequests += k.requestsCount || 0;
      totalTokens += k.tokensUsed || 0;
    });

    return {
      totalKeys: state.keys.length,
      activeKeys,
      totalRequests,
      totalTokens,
    };
  }, [state]);

  return (
    <div className="flex flex-col gap-6">
      {/* 1-Click Key Configuration Modal */}
      <KeyConfigModal apiKey={configKey} onClose={() => setConfigKey(null)} />

      {revealed ? <KeyOnboarding key={revealed.details.id} created={revealed} onDone={() => setRevealed(null)} /> : null}

      {/* Per-Key Summary Analytics Bar */}
      {state.status === 'ready' && state.keys.length > 0 ? (
        <div className="grid gap-4 sm:grid-cols-3">
          <StatCard
            label={t('metaTitle')}
            value={`${totals.activeKeys} / ${totals.totalKeys}`}
            hint={t('activeKeysCount', { count: totals.activeKeys })}
            tone="accent"
          />
          <StatCard
            label={t('totalRequestsMade')}
            value={compact(totals.totalRequests, locale)}
            hint="Tổng lượt gọi API phục vụ qua các keys"
          />
          <StatCard
            label={t('totalTokensUsed')}
            value={compact(totals.totalTokens, locale)}
            hint="Tổng lượng token xử lý qua gateway"
          />
        </div>
      ) : null}

      <Panel as="section">
        <PanelHeader title={t('createTitle')} description={t('createDescription')} />
        <form onSubmit={createKey} className="flex flex-wrap items-end gap-3 px-5 py-5">
          <div className="min-w-[14rem] flex-1">
            <label htmlFor="key-name" className="mb-2 block text-ui text-muted">
              {t('nameLabel')}
            </label>
            <input
              id="key-name"
              type="text"
              value={name}
              maxLength={60}
              disabled={revealed !== null}
              onChange={(event) => setName(event.target.value)}
              placeholder={t('namePlaceholder')}
              className="w-full rounded-md border border-line bg-surface-hover px-4 py-2.5 text-ui text-fg placeholder:text-dim focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            />
          </div>
          <button
            type="submit"
            disabled={creating || !canCreate || revealed !== null}
            className="rounded-full bg-fg px-6 py-2.5 text-ui font-medium text-bg transition-colors hover:bg-white/90 disabled:pointer-events-none disabled:opacity-50"
          >
            {creating ? t('creating') : t('createButton')}
          </button>
          {!canCreate ? <p className="text-ui text-warn">{t('needVerified')}</p> : null}
          {revealed ? <p className="text-ui text-info">{t('finishOnboardingFirst')}</p> : null}
        </form>
        {createError ? (
          <div className="px-5 pb-5">
            <ErrorNotice message={createError} />
          </div>
        ) : null}
      </Panel>

      <Panel as="section">
        <PanelHeader title={t('listTitle')} description={t('listDescription')} />

        {state.status === 'loading' ? <SkeletonRows rows={3} label={t('loading')} /> : null}

        {state.status === 'error' ? (
          <div className="p-5">
            <ErrorNotice message={state.message} onRetry={reload} retryLabel={t('retry')} />
          </div>
        ) : null}

        {state.status === 'ready' && state.keys.length === 0 ? (
          <EmptyState title={t('emptyTitle')} description={t('emptyDescription')} />
        ) : null}

        {state.status === 'ready' && state.keys.length > 0 ? (
          <TableScroll>
            <thead>
              <tr>
                <Th>{t('colName')}</Th>
                <Th>{t('colKey')}</Th>
                <Th>{t('colStatus')}</Th>
                <Th align="right">{t('colRequests')}</Th>
                <Th align="right">{t('colTokens')}</Th>
                <Th>{t('colLastUsed')}</Th>
                <Th align="right">{t('colActions')}</Th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {state.keys.map((key) => {
                const isRevoked = key.status === 'revoked';
                return (
                  <tr key={key.id} className="transition-colors hover:bg-surface-hover">
                    <Td className="text-fg font-medium">{key.name || t('unnamed')}</Td>
                    <Td className="font-mono text-dim">
                      <span className="flex flex-wrap items-center gap-2">
                        {key.keyMasked}
                        {key.testMode ? <Badge tone="neutral">{t('testMode')}</Badge> : null}
                      </span>
                    </Td>
                    <Td>
                      <span className="flex flex-wrap items-center gap-2">
                        <Badge
                          tone={
                            isRevoked
                              ? 'danger'
                              : key.status === 'active'
                                ? 'accent'
                                : key.status === 'provisioning'
                                  ? 'info'
                                  : 'neutral'
                          }
                        >
                          {t(`status.${key.status}`)}
                        </Badge>
                        {key.syncState !== 'synced' ? (
                          <Badge tone={syncTone[key.syncState]} title={key.syncError}>
                            {t(`sync.${key.syncState}`)}
                          </Badge>
                        ) : null}
                      </span>
                    </Td>
                    <Td align="right" className="font-mono tabular-nums">
                      {compact(key.requestsCount, locale)}
                    </Td>
                    <Td align="right" className="font-mono tabular-nums">
                      {compact(key.tokensUsed, locale)}
                    </Td>
                    <Td className="whitespace-nowrap text-dim font-mono text-label">
                      {key.lastUsedAt ? dateTime(key.lastUsedAt, locale) : t('never')}
                    </Td>
                    <Td align="right">
                      {isRevoked ? (
                        <span className="text-dim text-label">
                          {t('revokedAt', {
                            when: key.revokedAt ? dateTime(key.revokedAt, locale) : '',
                          })}
                        </span>
                      ) : (
                        <div className="flex flex-wrap items-center justify-end gap-1.5">
                          {/* 1-Click Configure Button */}
                          <button
                            type="button"
                            onClick={() => setConfigKey(key)}
                            title={t('configureKey')}
                            className="inline-flex items-center gap-1 rounded-full border border-accent/40 bg-accent-soft px-2.5 py-1 font-mono text-micro font-medium text-accent-light hover:bg-accent/25 transition-colors"
                          >
                            <span>⚡</span>
                            <span>{t('configure')}</span>
                          </button>

                          {/* 1-Click Ledger Filter */}
                          <Link
                            href={`/console/usage?keyId=${key.id}`}
                            title={t('viewUsage')}
                            className="rounded-full border border-line px-2.5 py-1 font-mono text-micro text-muted hover:border-line hover:text-fg transition-colors"
                          >
                            {t('viewUsage')}
                          </Link>

                          {/* Toggle Active */}
                          <button
                            type="button"
                            disabled={busyId === key.id}
                            onClick={() => void toggleEnabled(key)}
                            className="rounded-full border border-line px-2.5 py-1 font-mono text-micro text-muted transition-colors hover:bg-white/10 hover:text-fg disabled:pointer-events-none disabled:opacity-40"
                          >
                            {key.enabled ? t('disable') : t('enable')}
                          </button>

                          {/* Revoke Key */}
                          <button
                            type="button"
                            disabled={busyId === key.id}
                            onClick={() => void revokeKey(key)}
                            className="rounded-full border border-danger/40 px-2.5 py-1 font-mono text-micro text-danger transition-colors hover:bg-danger/10 disabled:pointer-events-none disabled:opacity-40"
                          >
                            {t('revoke')}
                          </button>
                        </div>
                      )}
                    </Td>
                  </tr>
                );
              })}
            </tbody>
          </TableScroll>
        ) : null}
      </Panel>
    </div>
  );
}
