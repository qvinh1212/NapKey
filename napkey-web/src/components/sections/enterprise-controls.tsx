import { useTranslations } from 'next-intl';
import { Section } from '@/components/ui/section';

const CONTROLS = ['zeroLogging', 'hardCaps', 'smartRouting', 'keyScoping'] as const;

export function EnterpriseControls() {
  const t = useTranslations('enterpriseControls');

  return (
    <Section id="enterprise" eyebrow={t('eyebrow')} title={t('title')} subtitle={t('subtitle')}>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {CONTROLS.map((key, index) => (
          <div
            key={key}
            className="group relative flex flex-col justify-between rounded-xl border border-line bg-surface p-6 transition-all duration-200 hover:border-accent/40 hover:bg-surface-hover"
          >
            <div>
              <div className="flex items-center justify-between">
                <span className="font-mono text-label text-accent-light">
                  0{index + 1}
                </span>
                <span className="size-2 rounded-full bg-accent/60 group-hover:bg-accent group-hover:animate-pulse" />
              </div>
              <h3 className="mt-4 text-base font-semibold text-fg group-hover:text-accent-light transition-colors">
                {t(`features.${key}.title`)}
              </h3>
              <p className="mt-2 text-ui text-muted leading-relaxed">
                {t(`features.${key}.description`)}
              </p>
            </div>
            <div className="mt-6 border-t border-line/60 pt-3 font-mono text-micro text-dim">
              Verified Control
            </div>
          </div>
        ))}
      </div>
    </Section>
  );
}
