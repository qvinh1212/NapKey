import { setRequestLocale } from 'next-intl/server';
import type { Locale } from '@/i18n/routing';
import { Hero } from '@/components/sections/hero';
import { LiveSignals } from '@/components/sections/live-signals';
import { ValueProps } from '@/components/sections/value-props';
import { EcosystemMap } from '@/components/sections/ecosystem-map';
import { Integration } from '@/components/sections/integration';
import { PricingTable } from '@/components/sections/pricing-table';
import { Billing } from '@/components/sections/billing';
import { DeveloperTrust } from '@/components/sections/developer-trust';
import { EnterpriseControls } from '@/components/sections/enterprise-controls';
import { Compatibility } from '@/components/sections/compatibility';
import { FinalCta } from '@/components/sections/final-cta';
import { LaunchOffer } from '@/components/napkey/launch-offer';

export default async function HomePage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);

  return (
    <>
      <Hero />
      <LiveSignals />
      <ValueProps />
      <EcosystemMap />
      <Integration />
      <PricingTable />
      <Billing />
      <EnterpriseControls />
      <DeveloperTrust />
      <Compatibility />
      <FinalCta />
      <LaunchOffer />
    </>
  );
}
