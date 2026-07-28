import { isIP } from 'node:net';

// Traefik appends the actual peer on the right. Selecting only that position
// prevents a browser-supplied value on the left from rotating the trial key.
export function trustedClientIP(forwardedFor: string | null, realIP: string | null): string | null {
  if (forwardedFor) {
    const chain = forwardedFor.split(',').map((value) => value.trim()).filter(Boolean);
    const candidate = chain.at(-1);
    if (candidate && isIP(candidate) !== 0) return candidate;
  }
  const fallback = realIP?.trim();
  return fallback && isIP(fallback) !== 0 ? fallback : null;
}
