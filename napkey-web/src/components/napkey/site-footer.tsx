import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { Logo } from './logo';
import { ContrastToggle } from './contrast-toggle';

const groups = [
  { title: 'product', links: ['pricing', 'integrate', 'billing'] },
  { title: 'resources', links: ['docs', 'compatibility', 'trust', 'status', 'contact'] },
  { title: 'legal', links: ['terms', 'privacy'] },
] as const;

const hrefs: Record<string, string> = {
  pricing: '/#pricing',
  integrate: '/#integrate',
  billing: '/#billing',
  docs: '/docs',
  trust: '/trust',
  compatibility: '/compatibility',
  status: '/status',
  contact: '/#cta',
  terms: '/terms',
  privacy: '/privacy',
};

export function SiteFooter() {
  const t = useTranslations('footer');
  const year = new Date().getFullYear();

  return (
    <footer className="border-t border-line bg-surface-3 pt-14 pb-8 sm:pt-20 sm:pb-10">
      <div className="container-page">
        <div className="grid grid-cols-2 gap-x-6 gap-y-10 md:grid-cols-[1.5fr_repeat(3,1fr)] md:gap-12">
          <div className="col-span-2 md:col-span-1">
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
                    <Link
                      href={hrefs[link]!}
                      className="text-ui text-muted transition-colors duration-150 hover:text-fg"
                    >
                      {t(`links.${link}`)}
                    </Link>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        <div className="mt-16 flex flex-col gap-4 border-t border-line pt-8 text-label text-dim md:flex-row md:items-center md:justify-between">
          <div className="flex flex-wrap items-center gap-3">
            <p>
              &copy; {year} NapKey. {t('rights')}
            </p>
            <ContrastToggle />
          </div>
          <p className="max-w-xl md:text-right">{t('disclaimer')}</p>
        </div>
      </div>
    </footer>
  );
}
