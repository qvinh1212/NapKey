import type { ReactNode } from 'react';

/**
 * Root layout khong dat <html> - viec do do [locale]/layout.tsx lam,
 * vi thuoc tinh lang phai khop locale dang hien thi.
 */
export default function RootLayout({ children }: { children: ReactNode }) {
  return children;
}
