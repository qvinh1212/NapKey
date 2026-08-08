import { useTranslations } from 'next-intl';
import { Section } from '@/components/ui/section';
import {
  ecosystemClients,
  ecosystemInboundSignals,
  ecosystemModels,
  ecosystemOutboundSignals,
} from '@/lib/ecosystem-map';

function SignalPacket({ path, duration, delay }: { path: string; duration: number; delay: number }) {
  const timing = `${duration}s`;
  const start = `-${delay}s`;

  return (
    <g className="ecosystem-signal">
      <circle r="7" fill="#10b981" opacity=".25" filter="url(#napkey-signal-glow)">
        <animateMotion path={path} dur={timing} begin={start} repeatCount="indefinite" />
      </circle>
      <circle r="2.4" fill="#a7f3d0">
        <animateMotion path={path} dur={timing} begin={start} repeatCount="indefinite" />
      </circle>
    </g>
  );
}

function ClientRow({ client, name }: { client: (typeof ecosystemClients)[number]; name: string }) {
  return (
    <li className="flex min-h-14 items-center gap-3 rounded-lg border border-line bg-black/70 px-3.5 py-2.5 backdrop-blur-sm">
      <span className="grid size-8 shrink-0 place-items-center rounded-md border border-line bg-surface font-mono text-micro text-fg">
        {client.mark}
      </span>
      <span className="rounded border border-accent/30 bg-accent-soft px-2 py-0.5 font-mono text-micro tracking-[0.12em] text-accent-light uppercase">
        {client.badge}
      </span>
      <span className="min-w-0 truncate text-ui font-medium text-fg">{name}</span>
      <span className="ml-auto size-2 shrink-0 rounded-full bg-accent shadow-[0_0_12px_rgba(16,185,129,0.55)]" aria-hidden />
    </li>
  );
}

function ModelRow({ model, label }: { model: (typeof ecosystemModels)[number]; label: string }) {
  return (
    <li className="flex min-h-14 items-center gap-3 rounded-lg border border-line bg-black/70 px-3.5 py-2.5 backdrop-blur-sm">
      <span className="size-2 shrink-0 rounded-full bg-accent shadow-[0_0_12px_rgba(16,185,129,0.55)]" aria-hidden />
      <span className="grid size-8 shrink-0 place-items-center rounded-md border border-line bg-surface font-display text-sm font-semibold text-fg">
        {model.mark}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-ui font-medium text-fg">{model.id}</span>
        <span className="block font-mono text-micro tracking-[0.08em] text-dim uppercase">{label}</span>
      </span>
    </li>
  );
}

export function EcosystemMap() {
  const t = useTranslations('ecosystem');

  return (
    <Section id="models" eyebrow={t('eyebrow')} title={t('title')} subtitle={t('subtitle')} className="border-t border-line">
      <div className="relative overflow-hidden rounded-xl border border-line bg-[#050807] px-4 py-8 sm:px-6 lg:px-8">
        <div className="absolute inset-0 grid-backdrop opacity-50" aria-hidden />
        <div className="absolute inset-x-1/4 top-1/2 h-64 -translate-y-1/2 rounded-full bg-accent/10 blur-3xl" aria-hidden />

        <svg className="pointer-events-none absolute inset-0 hidden size-full lg:block" viewBox="0 0 1200 620" preserveAspectRatio="none" aria-hidden>
          <defs>
            <linearGradient id="napkey-line-left" x1="0" x2="1">
              <stop stopColor="#10b981" stopOpacity=".18" />
              <stop offset="1" stopColor="#10b981" stopOpacity=".72" />
            </linearGradient>
            <linearGradient id="napkey-line-right" x1="0" x2="1">
              <stop stopColor="#10b981" stopOpacity=".72" />
              <stop offset="1" stopColor="#10b981" stopOpacity=".18" />
            </linearGradient>
            <filter id="napkey-signal-glow" x="-300%" y="-300%" width="700%" height="700%">
              <feGaussianBlur stdDeviation="4" />
            </filter>
          </defs>
          {ecosystemInboundSignals.map((signal) => (
            <path key={signal.path} d={signal.path} fill="none" stroke="url(#napkey-line-left)" strokeWidth="1.4" />
          ))}
          {ecosystemOutboundSignals.map((signal) => (
            <path key={signal.path} d={signal.path} fill="none" stroke="url(#napkey-line-right)" strokeWidth="1.4" />
          ))}
          {ecosystemInboundSignals.map((signal) => <SignalPacket key={`in-${signal.path}`} {...signal} />)}
          {ecosystemOutboundSignals.map((signal) => <SignalPacket key={`out-${signal.path}`} {...signal} />)}
        </svg>

        <div className="relative grid items-center gap-8 lg:grid-cols-[minmax(0,1fr)_15rem_minmax(0,1fr)] lg:gap-14">
          <div>
            <p className="mb-4 font-mono text-label tracking-[0.14em] text-dim uppercase">{t('clientsLabel')}</p>
            <ul className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-1">
              {ecosystemClients.map((client) => <ClientRow key={client.id} client={client} name={t(`clients.${client.id}`)} />)}
            </ul>
          </div>

          <div className="flex flex-col items-center py-2 text-center">
            <div className="relative grid size-28 place-items-center rounded-[1.75rem] border border-accent/30 bg-[radial-gradient(circle_at_50%_35%,rgba(52,211,153,0.22),rgba(16,185,129,0.06)_58%,rgba(0,0,0,0)_72%)] shadow-[0_0_60px_rgba(16,185,129,0.18)]">
              <span className="ecosystem-hub-pulse absolute inset-2 rounded-[1.35rem] border border-accent/40" aria-hidden />
              <div className="grid size-16 place-items-center rounded-2xl bg-accent text-2xl font-semibold tracking-[-0.08em] text-black shadow-[0_0_28px_rgba(16,185,129,0.32)]">NK</div>
              <span className="absolute -right-1 -top-1 flex items-center gap-1 rounded-full border border-accent/30 bg-[#07100c] px-2 py-1 font-mono text-micro text-accent-light">
                <span className="size-1.5 rounded-full bg-accent" aria-hidden /> LIVE
              </span>
            </div>
            <h3 className="mt-5 text-2xl">NapKey</h3>
            <p className="mt-2 max-w-48 text-prose text-muted">{t('hubBody')}</p>
            <div className="mt-4 flex flex-wrap justify-center gap-2 font-mono text-micro tracking-[0.08em] text-dim uppercase">
              <span className="rounded-full border border-line px-2.5 py-1">Anthropic</span>
              <span className="rounded-full border border-line px-2.5 py-1">OpenAI</span>
              <span className="rounded-full border border-line px-2.5 py-1">VNĐ</span>
            </div>
          </div>

          <div>
            <p className="mb-4 font-mono text-label tracking-[0.14em] text-dim uppercase">{t('modelsLabel')}</p>
            <ul className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-1">
              {ecosystemModels.map((model) => <ModelRow key={model.id} model={model} label={t(`families.${model.family}`)} />)}
            </ul>
            <p className="mt-4 text-ui leading-relaxed text-dim">{t('catalogNote')}</p>
          </div>
        </div>
      </div>
    </Section>
  );
}
