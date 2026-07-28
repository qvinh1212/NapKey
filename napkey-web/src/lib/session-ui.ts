export type SessionUIStatus = 'loading' | 'authenticated' | 'anonymous';

type PublicAuthAction = {
  href: '/console' | '/signin';
  labelKey: 'dashboard' | 'signIn';
};

export function publicAuthAction(status: SessionUIStatus): PublicAuthAction | null {
  if (status === 'loading') return null;
  if (status === 'authenticated') return { href: '/console', labelKey: 'dashboard' };
  return { href: '/signin', labelKey: 'signIn' };
}

export function shouldRedirectFromSignIn(status: SessionUIStatus): boolean {
  return status === 'authenticated';
}
