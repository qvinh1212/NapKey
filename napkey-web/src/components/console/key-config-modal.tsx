'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import type { ApiKey } from '@/lib/api/types';
import { site } from '@/lib/site';
import type { DeveloperTool } from '@/lib/developer-tools';
import { CopyButton } from '@/components/ui/copy-button';

const TOOL_OPTIONS: Array<{ id: DeveloperTool; label: string }> = [
  { id: 'claudeCode', label: 'Claude Code' },
  { id: 'cursor', label: 'Cursor' },
  { id: 'cline', label: 'Cline / Roo' },
  { id: 'windsurf', label: 'Windsurf' },
  { id: 'langchain', label: 'LangChain' },
  { id: 'anthropic', label: 'Anthropic SDK' },
  { id: 'openai', label: 'OpenAI SDK' },
  { id: 'curl', label: 'cURL' },
];

interface KeyConfigModalProps {
  apiKey: ApiKey | null;
  onClose: () => void;
}

export function KeyConfigModal({ apiKey, onClose }: KeyConfigModalProps) {
  const t = useTranslations('console.keys');
  const [selectedTool, setSelectedTool] = useState<DeveloperTool>('claudeCode');

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  if (!apiKey) return null;

  const keyPlaceholder = 'YOUR_API_KEY';
  const apiBaseUrl = site.apiBaseUrl;

  function getSnippetForTool(tool: DeveloperTool): { code: string; filename?: string; hint: string } {
    switch (tool) {
      case 'claudeCode':
        return {
          filename: 'Terminal (~/.bashrc or ~/.zshrc)',
          code: `export ANTHROPIC_BASE_URL="${apiBaseUrl}"\nexport ANTHROPIC_AUTH_TOKEN="${keyPlaceholder}"\nclaude`,
          hint: 'Chạy các lệnh trên trong terminal trước khi khởi chạy lệnh claude.',
        };
      case 'cursor':
        return {
          filename: 'Cursor Settings / Models / Custom OpenAI API Key',
          code: `Base URL: ${apiBaseUrl}/v1\nAPI Key:  ${keyPlaceholder}\nModel:    claude-sonnet-5`,
          hint: 'Dán Base URL và API Key vào phần Custom OpenAI API Key trong cài đặt của Cursor.',
        };
      case 'cline':
        return {
          filename: 'Cline Settings / API Provider: Anthropic Compatible',
          code: `Base URL: ${apiBaseUrl}\nAPI Key:  ${keyPlaceholder}\nModel ID: claude-sonnet-5`,
          hint: 'Chọn Anthropic Compatible trong Cline extension trên VS Code.',
        };
      case 'windsurf':
        return {
          filename: 'Windsurf Cascade Settings / Custom Model Endpoint',
          code: `API URL:  ${apiBaseUrl}/v1\nAPI Key:  ${keyPlaceholder}\nModel:    claude-sonnet-5`,
          hint: 'Nhập thông số vào Cascade Model Settings trên Windsurf.',
        };
      case 'langchain':
        return {
          filename: 'langchain_client.py',
          code: `from langchain_anthropic import ChatAnthropic\n\nllm = ChatAnthropic(\n    model="claude-sonnet-5",\n    anthropic_api_key="${keyPlaceholder}",\n    anthropic_api_url="${apiBaseUrl}",\n)\nresponse = llm.invoke("Xin chào NapKey!")`,
          hint: 'Tương thích hoàn hảo với LangChain Python và LangChain.js.',
        };
      case 'anthropic':
        return {
          filename: 'index.ts',
          code: `import Anthropic from '@anthropic-ai/sdk';\n\nconst client = new Anthropic({\n  baseURL: '${apiBaseUrl}',\n  apiKey: '${keyPlaceholder}',\n});\n\nconst message = await client.messages.create({\n  model: 'claude-sonnet-5',\n  max_tokens: 1024,\n  messages: [{ role: 'user', content: 'Hello via NapKey!' }],\n});`,
          hint: 'Sử dụng trực tiếp thư viện chính thức @anthropic-ai/sdk.',
        };
      case 'openai':
        return {
          filename: 'client.py',
          code: `from openai import OpenAI\n\nclient = OpenAI(\n    base_url="${apiBaseUrl}/v1",\n    api_key="${keyPlaceholder}",\n)\n\nresponse = client.chat.completions.create(\n    model="claude-sonnet-5",\n    messages=[{"role": "user", "content": "Hello via NapKey!"}],\n)`,
          hint: 'Sử dụng qua giao thức tương thích OpenAI SDK.',
        };
      case 'curl':
        return {
          filename: 'curl_request.sh',
          code: `curl -X POST "${apiBaseUrl}/v1/messages" \\\n  -H "x-api-key: ${keyPlaceholder}" \\\n  -H "anthropic-version: 2023-06-01" \\\n  -H "content-type: application/json" \\\n  -d '{\n    "model": "claude-sonnet-5",\n    "max_tokens": 1024,\n    "messages": [{"role": "user", "content": "Hello!"}]\n  }'`,
          hint: 'Kiểm tra phản hồi nhanh qua terminal bằng lệnh curl.',
        };
      default:
        return {
          code: `export ANTHROPIC_BASE_URL="${apiBaseUrl}"\nexport ANTHROPIC_AUTH_TOKEN="${keyPlaceholder}"`,
          hint: 'Cấu hình biến môi trường chuẩn.',
        };
    }
  }

  const activeSnippet = getSnippetForTool(selectedTool);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="key-config-modal-title"
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/75 backdrop-blur-md"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="relative w-full max-w-2xl overflow-hidden rounded-2xl border border-line bg-surface-3 shadow-[0_8px_24px_rgba(0,0,0,0.35)] animate-in fade-in zoom-in-95 duration-150">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-line px-6 py-4">
          <div className="flex items-center gap-3">
            <span className="flex size-9 items-center justify-center rounded-lg border border-accent/40 bg-accent-soft text-lg">
              🔑
            </span>
            <div>
              <h2 id="key-config-modal-title" className="font-mono text-ui font-semibold text-fg">
                {t('quickConfigTitle', { name: apiKey.name || apiKey.keyMasked })}
              </h2>
              <p className="font-mono text-micro text-dim">
                ID: {apiKey.id} · Mask: {apiKey.keyMasked}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="rounded-full border border-line bg-surface p-1.5 text-muted hover:bg-surface-hover hover:text-fg"
          >
            ✕
          </button>
        </div>

        {/* Content */}
        <div className="p-6">
          <p className="text-ui text-muted mb-4">{t('quickConfigDesc')}</p>

          {/* Tool Tabs */}
          <div className="mb-4 flex gap-1.5 overflow-x-auto border-b border-line pb-2 [scrollbar-width:thin]">
            {TOOL_OPTIONS.map((tool) => (
              <button
                key={tool.id}
                type="button"
                onClick={() => setSelectedTool(tool.id)}
                className={`shrink-0 rounded-lg px-3 py-1.5 font-mono text-micro font-medium transition-all ${
                  selectedTool === tool.id
                    ? 'border border-accent/40 bg-accent-soft text-accent-light'
                    : 'border border-transparent text-dim hover:bg-surface-hover hover:text-muted'
                }`}
              >
                {tool.label}
              </button>
            ))}
          </div>

          {/* Snippet Header */}
          <div className="flex items-center justify-between rounded-t-xl border border-line bg-surface px-4 py-2.5">
            <span className="font-mono text-micro text-dim truncate">{activeSnippet.filename}</span>
            <CopyButton value={activeSnippet.code} label="Copy snippet" variant="pill" showTooltip />
          </div>

          {/* Code Body */}
          <pre className="overflow-x-auto rounded-b-xl border-x border-b border-line bg-terminal p-4 font-mono text-[0.75rem] leading-6 text-zinc-300">
            <code>{activeSnippet.code}</code>
          </pre>

          <p className="mt-3 font-mono text-micro text-dim">💡 {activeSnippet.hint}</p>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between border-t border-line bg-surface/50 px-6 py-3 text-micro font-mono text-dim">
          <span>Thay {keyPlaceholder} bằng chuỗi secret key đầy đủ bạn đã lưu lúc tạo key.</span>
          <button
            type="button"
            onClick={onClose}
            className="rounded-full border border-line bg-surface px-4 py-1.5 text-fg hover:bg-surface-hover"
          >
            Đóng
          </button>
        </div>
      </div>
    </div>
  );
}
