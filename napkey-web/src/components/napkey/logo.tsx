export function Logo({ className = '' }: { className?: string }) {
  return (
    <span className={`inline-flex items-baseline gap-2 ${className}`}>
      <span
        aria-hidden
        className="size-2 shrink-0 translate-y-[-1px] rounded-full bg-accent animate-pulse-dot"
      />
      <span className="font-display text-base font-semibold tracking-[-0.03em]">NapKey</span>
    </span>
  );
}
