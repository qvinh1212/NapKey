import type { PublicModel } from './model-catalog';

export type DeveloperTool = 'claudeCode' | 'anthropic' | 'openai' | 'curl' | 'powershell';

export type DeveloperSnippet = { lang: string; code: string };

function cleanBaseUrl(value: string) {
  return value.replace(/\/+$/, '');
}

export function normalizeDeveloperModel(selected: string, models: Pick<PublicModel, 'id'>[]) {
  if (models.some((model) => model.id === selected)) return selected;
  return models.find((model) => model.id === 'auto')?.id ?? models[0]?.id ?? 'auto';
}

export function developerSnippet(tool: DeveloperTool, model: string, apiBaseUrl: string): DeveloperSnippet {
  const base = cleanBaseUrl(apiBaseUrl);
  const snippets: Record<DeveloperTool, DeveloperSnippet> = {
    claudeCode: {
      lang: 'bash',
      code: `export ANTHROPIC_BASE_URL="${base}"
export ANTHROPIC_AUTH_TOKEN="$NAPKEY_API_KEY"

claude --model "${model}"`,
    },
    anthropic: {
      lang: 'python',
      code: `import os
from anthropic import Anthropic

client = Anthropic(
    base_url="${base}",
    api_key=os.environ["NAPKEY_API_KEY"],
)

message = client.messages.create(
    model="${model}",
    max_tokens=256,
    messages=[{"role": "user", "content": "Reply with: NapKey ready"}],
)
print(message.content[0].text)`,
    },
    openai: {
      lang: 'typescript',
      code: `import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: '${base}/v1',
  apiKey: process.env.NAPKEY_API_KEY,
});

const response = await client.chat.completions.create({
  model: '${model}',
  messages: [{ role: 'user', content: 'Reply with: NapKey ready' }],
});
console.log(response.choices[0]?.message.content);`,
    },
    curl: {
      lang: 'bash',
      code: `curl "${base}/v1/messages" \\
  -H "x-api-key: $NAPKEY_API_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "content-type: application/json" \\
  -d '{
    "model": "${model}",
    "max_tokens": 64,
    "messages": [{"role": "user", "content": "Reply with: NapKey ready"}]
  }'`,
    },
    powershell: {
      lang: 'powershell',
      code: `$headers = @{
  'x-api-key' = $env:NAPKEY_API_KEY
  'anthropic-version' = '2023-06-01'
}

$body = @{
  model = '${model}'
  max_tokens = 64
  messages = @(
    @{ role = 'user'; content = 'Reply with: NapKey ready' }
  )
} | ConvertTo-Json -Depth 5

$request = @{
  Method = 'Post'
  Uri = '${base}/v1/messages'
  Headers = $headers
  ContentType = 'application/json'
  Body = $body
}

Invoke-RestMethod @request`,
    },
  };
  return snippets[tool];
}

export type FailureDiagnosis = {
  key: 'request' | 'authentication' | 'balance' | 'rateLimit' | 'upstream';
  retryable: boolean;
};

export function diagnoseApiFailure(status: number): FailureDiagnosis {
  if (status === 401 || status === 403) return { key: 'authentication', retryable: false };
  if (status === 402) return { key: 'balance', retryable: false };
  if (status === 429) return { key: 'rateLimit', retryable: true };
  if (status >= 500) return { key: 'upstream', retryable: true };
  return { key: 'request', retryable: false };
}
