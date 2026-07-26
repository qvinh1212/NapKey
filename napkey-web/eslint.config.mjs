import nextCoreWebVitals from 'eslint-config-next/core-web-vitals';
import nextTypescript from 'eslint-config-next/typescript';

/**
 * eslint-config-next 16 xuat flat config truc tiep, khong can FlatCompat.
 */
const config = [
  { ignores: ['.next*/**', 'node_modules/**', 'next-env.d.ts'] },
  ...nextCoreWebVitals,
  ...nextTypescript,
];

export default config;
