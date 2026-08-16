import type { ReactNode } from 'react';

type SectionProps = {
  id?: string;
  eyebrow?: string;
  title?: string;
  subtitle?: string;
  children: ReactNode;
  className?: string;
  /**
   * Bo khoang cach tren de khoi nay doc lien voi khoi ngay truoc.
   *
   * Trang chu tung co 10 khoi lien tiep cung mot nhip 8rem, khien nguoi doc mat
   * moc dinh huong. `joined` cho phep ghep hai khoi lien quan (gia + thanh toan,
   * tin cay + tuong thich) thanh mot don vi thi giac ma van giu id rieng, vi
   * thanh dieu huong tro truc tiep vao #billing va #compatibility.
   */
  joined?: boolean;
};

export function Section({
  id,
  eyebrow,
  title,
  subtitle,
  children,
  className = '',
  joined = false,
}: SectionProps) {
  const headingId = id ? `${id}-heading` : undefined;

  return (
    <section
      id={id}
      aria-labelledby={headingId}
      className={`${joined ? 'section-y-joined' : 'section-y'} ${className}`}
    >
      <div className="container-page">
        {(eyebrow ?? title ?? subtitle) ? (
          <header className={`max-w-3xl ${joined ? 'mb-8 sm:mb-10' : 'mb-10 sm:mb-16'}`}>
            {eyebrow ? (
              <p className="mb-4 font-mono text-label font-semibold tracking-[0.14em] text-accent uppercase">
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
