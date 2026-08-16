'use client';

import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { useRouter } from '@/i18n/navigation';

export interface CommandItem {
  id: string;
  title: string;
  description?: string;
  category: 'actions' | 'navigation' | 'resources';
  shortcut?: string;
  icon: string;
  action: () => void;
}

interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
  onOpenTopup: () => void;
  onSignOut: () => void;
  hasAdminPermission?: boolean;
}

export function CommandPalette({
  open,
  onClose,
  onOpenTopup,
  onSignOut,
  hasAdminPermission = false,
}: CommandPaletteProps) {
  const t = useTranslations('console.commandPalette');
  const router = useRouter();
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const listboxId = useId();

  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);

  function toggleHighContrast() {
    const isHigh = document.documentElement.classList.toggle('high-contrast');
    try {
      localStorage.setItem('napkey-contrast', isHigh ? 'high' : 'standard');
    } catch {
      // Ignore
    }
  }

  const allCommands = useMemo<CommandItem[]>(() => {
    const items: CommandItem[] = [
      // Quick Actions
      {
        id: 'topup',
        title: t('commands.topup'),
        description: t('commands.topupDesc'),
        category: 'actions',
        icon: '⚡',
        action: () => {
          onClose();
          onOpenTopup();
        },
      },
      {
        id: 'create-key',
        title: t('commands.createKey'),
        description: t('commands.createKeyDesc'),
        category: 'actions',
        icon: '🔑',
        action: () => {
          onClose();
          router.push('/console/keys');
        },
      },
      {
        id: 'configure',
        title: t('commands.configure'),
        description: t('commands.configureDesc'),
        category: 'actions',
        icon: '🛠️',
        action: () => {
          onClose();
          router.push('/console/developer');
        },
      },
      {
        id: 'export-csv',
        title: t('commands.exportCsv'),
        description: t('commands.exportCsvDesc'),
        category: 'actions',
        icon: '📊',
        action: () => {
          onClose();
          router.push('/console/usage');
        },
      },
      {
        id: 'contrast',
        title: t('commands.contrast'),
        description: t('commands.contrastDesc'),
        category: 'actions',
        icon: '🌓',
        action: () => {
          onClose();
          toggleHighContrast();
        },
      },

      // Navigation
      {
        id: 'nav-overview',
        title: t('commands.overview'),
        category: 'navigation',
        icon: '🏠',
        action: () => {
          onClose();
          router.push('/console');
        },
      },
      {
        id: 'nav-usage',
        title: t('commands.usage'),
        category: 'navigation',
        icon: '📈',
        action: () => {
          onClose();
          router.push('/console/usage');
        },
      },
      {
        id: 'nav-wallet',
        title: t('commands.wallet'),
        category: 'navigation',
        icon: '💳',
        action: () => {
          onClose();
          router.push('/console/billing');
        },
      },
      {
        id: 'nav-keys',
        title: t('commands.keys'),
        category: 'navigation',
        icon: '🗝️',
        action: () => {
          onClose();
          router.push('/console/keys');
        },
      },
      {
        id: 'nav-developer',
        title: t('commands.developer'),
        category: 'navigation',
        icon: '💻',
        action: () => {
          onClose();
          router.push('/console/developer');
        },
      },
      {
        id: 'nav-settings',
        title: t('commands.settings'),
        category: 'navigation',
        icon: '⚙️',
        action: () => {
          onClose();
          router.push('/console/settings');
        },
      },

      // Resources & External
      {
        id: 'res-docs',
        title: t('commands.docs'),
        category: 'resources',
        icon: '📖',
        action: () => {
          onClose();
          router.push('/docs');
        },
      },
      {
        id: 'res-status',
        title: t('commands.status'),
        category: 'resources',
        icon: '🟢',
        action: () => {
          onClose();
          router.push('/status');
        },
      },
      {
        id: 'res-pricing',
        title: t('commands.pricing'),
        category: 'resources',
        icon: '📋',
        action: () => {
          onClose();
          router.push('/#pricing');
        },
      },
      {
        id: 'res-signout',
        title: t('commands.signOut'),
        category: 'resources',
        icon: '🚪',
        action: () => {
          onClose();
          onSignOut();
        },
      },
    ];

    if (hasAdminPermission) {
      items.push({
        id: 'nav-admin',
        title: t('commands.admin'),
        category: 'navigation',
        icon: '🛡️',
        action: () => {
          onClose();
          router.push('/console/admin');
        },
      });
    }

    return items;
  }, [t, router, onClose, onOpenTopup, onSignOut, hasAdminPermission]);

  const filteredCommands = useMemo(() => {
    if (!query.trim()) return allCommands;
    const q = query.trim().toLowerCase();
    return allCommands.filter((cmd) => {
      return (
        cmd.title.toLowerCase().includes(q) ||
        (cmd.description && cmd.description.toLowerCase().includes(q))
      );
    });
  }, [allCommands, query]);

  // Focus input when modal opens
  useEffect(() => {
    if (open) {
      inputRef.current?.focus();
    }
  }, [open]);

  // Keyboard navigation inside palette
  useEffect(() => {
    if (!open) return;

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault();
        onClose();
        return;
      }

      if (event.key === 'ArrowDown') {
        event.preventDefault();
        setSelectedIndex((prev) => (prev + 1) % Math.max(1, filteredCommands.length));
        return;
      }

      if (event.key === 'ArrowUp') {
        event.preventDefault();
        setSelectedIndex((prev) =>
          prev <= 0 ? filteredCommands.length - 1 : prev - 1
        );
        return;
      }

      if (event.key === 'Enter') {
        event.preventDefault();
        if (filteredCommands[selectedIndex]) {
          filteredCommands[selectedIndex].action();
        }
      }
    }

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, filteredCommands, selectedIndex, onClose]);

  if (!open) return null;

  const categories: Array<'actions' | 'navigation' | 'resources'> = [
    'actions',
    'navigation',
    'resources',
  ];

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="command-palette-title"
      className="fixed inset-0 z-50 flex items-start justify-center p-4 pt-16 sm:pt-24 bg-black/75 backdrop-blur-md transition-opacity"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="relative w-full max-w-xl overflow-hidden rounded-2xl border border-line bg-surface-3 shadow-[0_8px_24px_rgba(0,0,0,0.35)] animate-in fade-in zoom-in-95 duration-150">
        <h2 id="command-palette-title" className="sr-only">
          {t('trigger')}
        </h2>

        {/* Search Input Bar */}
        <div className="flex items-center gap-3 border-b border-line px-4 py-3.5">
          <svg
            className="size-5 shrink-0 text-muted"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          >
            <circle cx="11" cy="11" r="8" />
            <line x1="21" y1="21" x2="16.65" y2="16.65" />
          </svg>
          <input
            ref={inputRef}
            type="text"
            role="combobox"
            aria-expanded="true"
            aria-controls={listboxId}
            aria-autocomplete="list"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedIndex(0);
            }}
            placeholder={t('placeholder')}
            className="w-full bg-transparent font-mono text-ui text-fg placeholder:text-dim outline-none"
          />
          {query ? (
            <button
              type="button"
              onClick={() => setQuery('')}
              className="text-micro font-mono text-dim hover:text-fg"
            >
              ✕
            </button>
          ) : (
            <kbd className="hidden sm:inline-flex rounded border border-line bg-surface px-1.5 py-0.5 font-mono text-micro text-dim">
              ESC
            </kbd>
          )}
        </div>

        {/* Results List */}
        <div ref={listRef} id={listboxId} role="listbox" className="max-h-[60vh] overflow-y-auto p-2">
          {filteredCommands.length === 0 ? (
            <div className="p-8 text-center font-mono text-label text-dim">
              {t('noResults')}
            </div>
          ) : (
            categories.map((category) => {
              const categoryCommands = filteredCommands.filter(
                (cmd) => cmd.category === category
              );
              if (categoryCommands.length === 0) return null;

              return (
                <div key={category} className="mb-2">
                  <div className="px-3 py-1.5 font-mono text-micro tracking-widest text-dim uppercase">
                    {t(`groups.${category}`)}
                  </div>
                  <div className="space-y-0.5">
                    {categoryCommands.map((command) => {
                      const globalIndex = filteredCommands.indexOf(command);
                      const isSelected = globalIndex === selectedIndex;

                      return (
                        <button
                          key={command.id}
                          role="option"
                          aria-selected={isSelected}
                          type="button"
                          onClick={() => command.action()}
                          onMouseEnter={() => setSelectedIndex(globalIndex)}
                          className={`group flex w-full items-center justify-between gap-3 rounded-xl px-3 py-2.5 text-left transition-all ${
                            isSelected
                              ? 'bg-accent/15 text-fg border border-accent/40 shadow-sm'
                              : 'text-muted hover:bg-surface-hover border border-transparent'
                          }`}
                        >
                          <div className="flex items-center gap-3 min-w-0">
                            <span className="text-base shrink-0">{command.icon}</span>
                            <div className="min-w-0">
                              <div
                                className={`font-mono text-ui font-medium truncate ${
                                  isSelected ? 'text-accent-light' : 'text-fg'
                                }`}
                              >
                                {command.title}
                              </div>
                              {command.description ? (
                                <div className="font-mono text-micro text-dim truncate">
                                  {command.description}
                                </div>
                              ) : null}
                            </div>
                          </div>
                          {isSelected ? (
                            <span className="shrink-0 font-mono text-micro text-accent-light">
                              ↵
                            </span>
                          ) : null}
                        </button>
                      );
                    })}
                  </div>
                </div>
              );
            })
          )}
        </div>

        {/* Footer Navigation Hints */}
        <div className="flex items-center justify-between border-t border-line/60 bg-surface/50 px-4 py-2 text-micro font-mono text-dim">
          <div className="flex items-center gap-3">
            <span>
              <kbd className="rounded border border-line bg-surface px-1 py-0.5">↑</kbd>{' '}
              <kbd className="rounded border border-line bg-surface px-1 py-0.5">↓</kbd> để di chuyển
            </span>
            <span>
              <kbd className="rounded border border-line bg-surface px-1.5 py-0.5">↵</kbd> để chọn
            </span>
          </div>
          <span className="text-accent-light">NapKey Console ProMax</span>
        </div>
      </div>
    </div>
  );
}
