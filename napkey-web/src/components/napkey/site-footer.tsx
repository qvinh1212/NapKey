import { useTranslations } from 'next-intl';
import { Logo } from './logo';

const groups = [
  { title: 'product', links: ['pricing', 'integrate', 'billing'] },
  { title: 'resources', links: ['status', 'contact'] },
  { title: 'legal', links: ['terms', 'privacy'] },
] as const;

const hrefs: Record<string, string> = {
  pricing: '#pricing',
  integrate: '#integrate',
  billing: '#billing',
  status: '#billing',
  contact: '#cta',
  terms: '#billing',
  privacy: '#billing',
};

export function SiteFooter() {
  const t = useTranslations('footer');
  const year = new Date().getFullYear();

  return (
    <footer className="border-t border-zinc-900 bg-black pt-20 pb-10">
      <div className="container-page">
        <div className="grid gap-12 md:grid-cols-[1.5fr_repeat(3,1fr)]">
          <div>
            <Logo />
            <p className="mt-4 max-w-xs text-ui leading-relaxed text-muted">{t('tagline')}</p>
          </div>

          {groups.map((group) => (
            <div key={group.title}>
              <h3 className="mb-4 font-mono text-label tracking-[0.16em] text-dim uppercase">
                {t(group.title)}
              </h3>
              <ul className="space-y-3">
                {group.links.map((link) => (
                  <li key={link}>
                    <a
                      href={hrefs[link]}
                      className="text-ui text-muted transition-colors duration-150 hover:text-fg"
                    >
                      {t(`links.${link}`)}
                    </a>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        <div className="mt-16 flex flex-col gap-4 border-t border-zinc-900 pt-8 text-label text-dim md:flex-row md:items-center md:justify-between">
          <p>
            &copy; {year} NapKey. {t('rights')}
          </p>
          <p className="max-w-xl md:text-right">{t('disclaimer')}</p>
        </div>
      </div>
    </footer>
  );
}
