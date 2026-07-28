import type { NextConfig } from 'next';
import createNextIntlPlugin from 'next-intl/plugin';
import { webSecurityHeaders } from './src/lib/security-headers';

const withNextIntl = createNextIntlPlugin('./src/i18n/request.ts');

const nextConfig: NextConfig = {
	distDir: process.env.NEXT_DIST_DIR || '.next',
  output: 'standalone',
  poweredByHeader: false,
  reactStrictMode: true,
  async headers() {
    return [
      {
        source: '/:path*',
        headers: webSecurityHeaders(process.env.NEXT_PUBLIC_API_BASE_URL ?? 'https://api.napkey.io.vn'),
      },
    ];
  },
};

export default withNextIntl(nextConfig);
