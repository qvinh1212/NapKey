import { setRequestLocale } from 'next-intl/server';
import type { Locale } from '@/i18n/routing';
import { Hero } from '@/components/sections/hero';
import { ValueProps } from '@/components/sections/value-props';
import { Integration } from '@/components/sections/integration';
import { PricingTable } from '@/components/sections/pricing-table';
import { Billing } from '@/components/sections/billing';
import { DeveloperTrust } from '@/components/sections/developer-trust';
import { FinalCta } from '@/components/sections/final-cta';

export default async function HomePage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);

  return (
    <>
      <Hero />
      <ValueProps />
      <Integration />
      <PricingTable />
      <Billing />
      <DeveloperTrust />
      <FinalCta />
    </>
  );
}
