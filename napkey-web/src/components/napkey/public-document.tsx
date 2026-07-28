import type { ReactNode } from 'react';

export function PublicDocument({ eyebrow, title, intro, children }: { eyebrow: string; title: string; intro: string; children: ReactNode }) {
  return (
    <div className="container-page pt-40 pb-28">
      <header className="max-w-3xl border-b border-line pb-12">
        <p className="font-mono text-label tracking-[0.18em] text-accent uppercase">{eyebrow}</p>
        <h1 className="mt-5 text-4xl leading-tight tracking-[-0.03em] text-balance sm:text-6xl">{title}</h1>
        <p className="mt-6 max-w-2xl text-base leading-relaxed text-muted">{intro}</p>
      </header>
      <div className="mt-12 max-w-4xl">{children}</div>
    </div>
  );
}

export function DocumentSection({ index, title, children }: { index: string; title: string; children: ReactNode }) {
  return (
    <section className="grid gap-4 border-b border-line py-9 md:grid-cols-[4rem_15rem_1fr] md:gap-7">
      <span aria-hidden className="font-mono text-label text-dim">{index}</span>
      <h2 className="text-lg tracking-[-0.01em] text-fg">{title}</h2>
      <div className="space-y-4 text-ui leading-relaxed text-muted">{children}</div>
    </section>
  );
}
