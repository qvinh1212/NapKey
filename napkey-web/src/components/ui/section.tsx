import type { ReactNode } from 'react';

type SectionProps = {
  id?: string;
  eyebrow?: string;
  title?: string;
  subtitle?: string;
  children: ReactNode;
  className?: string;
};

export function Section({ id, eyebrow, title, subtitle, children, className = '' }: SectionProps) {
  const headingId = id ? `${id}-heading` : undefined;

  return (
    <section id={id} aria-labelledby={headingId} className={`section-y ${className}`}>
      <div className="container-page">
        {(eyebrow ?? title ?? subtitle) ? (
          <header className="mb-10 max-w-3xl sm:mb-16">
            {eyebrow ? (
              <p className="mb-4 font-mono text-label tracking-[0.18em] text-accent uppercase">
                {eyebrow}
              </p>
            ) : null}
            {title ? (
              <h2
                id={headingId}
                className="text-3xl leading-[1.08] tracking-[-0.02em] text-balance sm:text-4xl lg:text-5xl"
              >
                {title}
              </h2>
            ) : null}
            {subtitle ? (
              <p className="mt-5 max-w-2xl text-base leading-relaxed text-muted">{subtitle}</p>
            ) : null}
          </header>
        ) : null}
        {children}
      </div>
    </section>
  );
}
