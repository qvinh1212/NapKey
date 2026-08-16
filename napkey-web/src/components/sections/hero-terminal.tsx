'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { site } from '@/lib/site';
import { CopyButton } from '@/components/ui/copy-button';

const PROMPT_SNIPPETS = {
  debounce: {
    id: 'debounce',
    model: 'claude-sonnet-5',
    tokens: 118,
    costVnd: '4.00 CR',
    code: `// TypeScript Debounce Hook
import { useState, useEffect } from 'react';

export function useDebounce<T>(value: T, delayMs = 300): T {
  const [debounced, setDebounced] = useState<T>(value);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);

  return debounced;
}`,
  },
  sql: {
    id: 'sql',
    model: 'claude-sonnet-5',
    tokens: 145,
    costVnd: '4.00 CR',
    code: `-- Optimized Index Scan Query
WITH active_keys AS (
  SELECT id, user_id, last_used_at
  FROM api_keys
  WHERE status = 'active'
  ORDER BY last_used_at DESC
  LIMIT 50
)
SELECT k.id, k.user_id, count(u.id) as request_count
FROM active_keys k
LEFT JOIN usage_records u ON u.key_id = k.id
GROUP BY k.id, k.user_id;`,
  },
  auth: {
    id: 'auth',
    model: 'claude-sonnet-5',
    tokens: 132,
    costVnd: '4.01 CR',
    code: `// Next.js Edge Auth Middleware
import { NextResponse, type NextRequest } from 'next/server';

export function middleware(req: NextRequest) {
  const token = req.cookies.get('session_token')?.value;
  if (!token && req.nextUrl.pathname.startsWith('/console')) {
    return NextResponse.redirect(new URL('/signin', req.url));
  }
  return NextResponse.next();
}`,
  },
} as const;

type PromptKey = keyof typeof PROMPT_SNIPPETS;

export function HeroTerminal() {
  const t = useTranslations('hero');
  const [tab, setTab] = useState<'setup' | 'sandbox'>('setup');
  const [selectedPrompt, setSelectedPrompt] = useState<PromptKey>('debounce');
  const [streamedText, setStreamedText] = useState('');
  const [isStreaming, setIsStreaming] = useState(false);
  const [progressToken, setProgressToken] = useState(0);
  const timerRef = useRef<NodeJS.Timeout | null>(null);

  const activeSnippet = PROMPT_SNIPPETS[selectedPrompt];

  const setupLines = [
    ['muted', `# ${t('terminalComment')}`],
    ['plain', `export ANTHROPIC_BASE_URL="${site.apiBaseUrl}"`],
    ['accent', 'export ANTHROPIC_AUTH_TOKEN="nk_live_..."'],
    ['plain', 'claude'],
  ] as const;

  const rawSetupSnippet = [
    `# ${t('terminalComment')}`,
    `export ANTHROPIC_BASE_URL="${site.apiBaseUrl}"`,
    'export ANTHROPIC_AUTH_TOKEN="nk_live_..."',
    'claude',
  ].join('\n');

  function startStreamSimulation() {
    if (isStreaming) return;
    if (timerRef.current) clearInterval(timerRef.current);

    setIsStreaming(true);
    setStreamedText('');
    setProgressToken(0);

    const fullCode = activeSnippet.code;
    let charIndex = 0;
    const chunkSize = 3;

    timerRef.current = setInterval(() => {
      charIndex += chunkSize;
      if (charIndex >= fullCode.length) {
        setStreamedText(fullCode);
        setIsStreaming(false);
        setProgressToken(activeSnippet.tokens);
        if (timerRef.current) clearInterval(timerRef.current);
      } else {
        setStreamedText(fullCode.slice(0, charIndex));
        setProgressToken(Math.round((charIndex / fullCode.length) * activeSnippet.tokens));
      }
    }, 20);
  }

  useEffect(() => {
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, []);

  return (
    <div className="relative overflow-hidden rounded-xl border border-white/15 bg-[#070908]/90 shadow-[0_32px_100px_rgba(0,0,0,0.65)] backdrop-blur">
      {/* Terminal Top Bar */}
      <div className="flex items-center justify-between border-b border-line px-4 py-3 sm:px-5">
        <div className="flex items-center gap-3">
          <div className="flex gap-2" aria-hidden>
            <span className="size-2.5 rounded-full bg-[#ff5f57]" />
            <span className="size-2.5 rounded-full bg-[#febc2e]" />
            <span className="size-2.5 rounded-full bg-[#28c840]" />
          </div>

          {/* Mode Tabs */}
          <div className="inline-flex rounded-lg border border-line bg-surface p-0.5 font-mono text-micro">
            <button
              type="button"
              onClick={() => setTab('setup')}
              className={`rounded-md px-2.5 py-1 transition-colors ${
                tab === 'setup' ? 'bg-accent/20 text-accent-light font-semibold' : 'text-dim hover:text-muted'
              }`}
            >
              {t('tabSetup')}
            </button>
            <button
              type="button"
              onClick={() => {
                setTab('sandbox');
                if (!streamedText) startStreamSimulation();
              }}
              className={`rounded-md px-2.5 py-1 transition-colors ${
                tab === 'sandbox' ? 'bg-accent/20 text-accent-light font-semibold' : 'text-dim hover:text-muted'
              }`}
            >
              ⚡ {t('tabSandbox')}
            </button>
          </div>
        </div>

        {/* Telemetry Indicator */}
        <div className="hidden sm:inline-flex items-center gap-1.5 rounded-full border border-accent/30 bg-accent-soft px-2 py-0.5 font-mono text-micro text-accent-light">
          <span className="size-1.5 rounded-full bg-accent animate-pulse" />
          <span>~310ms TTFT · 115 tok/s</span>
        </div>
      </div>

      {/* Terminal Body Content */}
      <div className="min-h-72 p-5 sm:p-6">
        {tab === 'setup' ? (
          <>
            <div className="mb-6 flex items-center justify-between border-b border-line pb-4">
              <div className="flex items-center gap-3">
                <span className="flex size-8 items-center justify-center rounded-md border border-accent/30 bg-accent-soft font-mono text-label text-accent-light">
                  NK
                </span>
                <div>
                  <p className="font-mono text-ui text-zinc-200">{t('terminalTitle')}</p>
                  <p className="font-mono text-micro text-muted">{t('terminalSubtitle')}</p>
                </div>
              </div>
              <CopyButton value={rawSetupSnippet} label="Copy" variant="pill" showTooltip />
            </div>

            <pre className="overflow-x-auto font-mono text-[0.75rem] leading-7 sm:text-ui text-zinc-300">
              <code>
                {setupLines.map(([tone, line]) => (
                  <span
                    key={line}
                    className={
                      'block ' +
                      (tone === 'accent'
                        ? 'text-accent-light'
                        : tone === 'muted'
                          ? 'text-muted'
                          : 'text-zinc-300')
                    }
                  >
                    <span aria-hidden className="mr-3 select-none text-zinc-700">
                      $
                    </span>
                    {line}
                  </span>
                ))}
              </code>
            </pre>
          </>
        ) : (
          /* Live Stream Sandbox Tab */
          <div>
            {/* Prompt Selector & Run Button */}
            <div className="mb-4 flex flex-wrap items-center justify-between gap-2.5 border-b border-line pb-3.5">
              <div className="flex flex-wrap items-center gap-1.5 font-mono text-micro">
                <span className="text-dim mr-1">{t('sandbox.promptLabel')}</span>
                {(['debounce', 'sql', 'auth'] as const).map((key) => (
                  <button
                    key={key}
                    type="button"
                    onClick={() => {
                      setSelectedPrompt(key);
                      setStreamedText('');
                    }}
                    className={`rounded-md border px-2 py-1 transition-all ${
                      selectedPrompt === key
                        ? 'border-accent/40 bg-accent-soft text-accent-light font-medium'
                        : 'border-line bg-surface text-dim hover:text-fg'
                    }`}
                  >
                    {t(`sandbox.prompts.${key}`)}
                  </button>
                ))}
              </div>

              <button
                type="button"
                onClick={startStreamSimulation}
                disabled={isStreaming}
                className="inline-flex items-center gap-1.5 rounded-full border border-accent/40 bg-accent-soft px-3 py-1 font-mono text-micro font-medium text-accent-light hover:bg-accent/25 transition-colors disabled:opacity-50"
              >
                <span>{isStreaming ? '⚡' : '▶'}</span>
                <span>{isStreaming ? t('sandbox.runningButton') : t('sandbox.runButton')}</span>
              </button>
            </div>

            {/* Simulated Streaming Output Canvas */}
            <div className="rounded-lg border border-line/60 bg-black/60 p-3.5 font-mono text-[0.73rem] sm:text-label text-zinc-300 min-h-40 overflow-x-auto">
              {streamedText ? (
                <pre className="whitespace-pre-wrap leading-relaxed">
                  <code>{streamedText}</code>
                  {isStreaming ? <span className="inline-block size-2 bg-accent ml-1 animate-pulse" /> : null}
                </pre>
              ) : (
                <div className="flex h-36 flex-col items-center justify-center text-center font-mono text-micro text-dim">
                  <p>Bấm &quot;Chạy thử Live Stream&quot; để quan sát tốc độ và chi phí theo thời gian thực.</p>
                </div>
              )}
            </div>

            {/* Live Telemetry Ticker Footer */}
            <div className="mt-3.5 grid grid-cols-4 gap-2 border-t border-line/60 pt-3 text-center font-mono">
              <div className="rounded border border-line/40 bg-surface/50 p-1.5">
                <div className="text-micro text-dim">{t('sandbox.ttftLabel')}</div>
                <div className="text-ui font-semibold text-accent-light">~310ms</div>
              </div>
              <div className="rounded border border-line/40 bg-surface/50 p-1.5">
                <div className="text-micro text-dim">{t('sandbox.speedLabel')}</div>
                <div className="text-ui font-semibold text-fg">118 tok/s</div>
              </div>
              <div className="rounded border border-line/40 bg-surface/50 p-1.5">
                <div className="text-micro text-dim">{t('sandbox.tokensLabel')}</div>
                <div className="text-ui font-semibold text-fg">{progressToken || activeSnippet.tokens}</div>
              </div>
              <div className="rounded border border-line/40 bg-surface/50 p-1.5">
                <div className="text-micro text-dim">{t('sandbox.costLabel')}</div>
                <div className="text-ui font-semibold text-accent-light">{activeSnippet.costVnd}</div>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Metrics Row Footer */}
      <div className="grid grid-cols-3 border-t border-line bg-white/[0.015]">
        {(['topUp', 'protocols', 'monthlyFee'] as const).map((key) => (
          <div key={key} className="min-w-0 border-r border-line px-2.5 py-3.5 last:border-r-0 sm:px-5">
            <p className="font-display text-base font-semibold tracking-[-0.04em] text-fg sm:text-xl">
              {t(`metrics.${key}.value`)}
            </p>
            <p className="mt-0.5 text-micro leading-snug text-muted sm:text-label">
              {t(`metrics.${key}.label`)}
            </p>
          </div>
        ))}
      </div>
    </div>
  );
}
