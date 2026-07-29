import type { ReactNode } from 'react';

/**
 * Bo cuc dung chung cho cac trang ngoai console: dang nhap, dang ky, xac minh
 * email, quen/dat lai mat khau.
 *
 * Nam trang dung dung mot khung nay. Lap lai o tung trang la nam cho de mot trang
 * lech ra ma khong ai de y.
 */
export function AuthShell({ children }: { children: ReactNode }) {
  return (
    <div className="container-page flex min-h-dvh items-center justify-center py-24 sm:py-28">
      <div className="w-full max-w-md">{children}</div>
    </div>
  );
}
