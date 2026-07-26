import type { MetadataRoute } from 'next';
import { locales } from '@/i18n/routing';
import { site } from '@/lib/site';

/**
 * Nhung khu vuc khong nen xuat hien tren ket qua tim kiem.
 *
 * Console va cac trang auth khong co noi dung cong khai; crawler doi vao day chi
 * tao ra mot loat 401. Cac trang nhan token con te hon: URL co token trong query,
 * khong co ly do gi de no nam trong mot chi muc tim kiem.
 */
const privatePaths = [
  '/console',
  '/signin',
  '/signup',
  '/forgot-password',
  '/reset-password',
  '/verify-email',
  '/resend-verification',
] as const;

export default function robots(): MetadataRoute.Robots {
  // Sinh theo locale thay vi viet tay: them mot ngon ngu se tu duoc bao ve.
  const disallow = ['/api/', ...locales.flatMap((l) => privatePaths.map((p) => `/${l}${p}`))];

  return {
    rules: [{ userAgent: '*', allow: '/', disallow }],
    sitemap: `${site.url}/sitemap.xml`,
  };
}