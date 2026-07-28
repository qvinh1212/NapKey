export type OnboardingTool = 'claudeCode' | 'anthropic' | 'openai' | 'curl';

export type OnboardingResult = {
  id: string;
  model: string;
  text: string;
  inputTokens: number;
  outputTokens: number;
};

function cleanBaseUrl(apiBaseUrl: string) {
  return apiBaseUrl.replace(/\/+$/, '');
}

export function onboardingSnippet(tool: OnboardingTool, secret: string, apiBaseUrl: string) {
  const base = cleanBaseUrl(apiBaseUrl);

  const snippets = {
    claudeCode: {
      lang: 'bash',
      code: `export ANTHROPIC_BASE_URL="${base}"
export ANTHROPIC_AUTH_TOKEN="${secret}"

claude`,
    },
    anthropic: {
      lang: 'python',
      code: `from anthropic import Anthropic

client = Anthropic(
    base_url="${base}",
    api_key="${secret}",
)

message = client.messages.create(
    model="claude-sonnet-4.6",
    max_tokens=128,
    messages=[{"role": "user", "content": "Reply with exactly: NapKey ready"}],
)
print(message.content[0].text)`,
    },
    openai: {
      lang: 'typescript',
      code: `import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: '${base}/v1',
  apiKey: '${secret}',
});

const response = await client.chat.completions.create({
  model: 'claude-sonnet-4.6',
  messages: [{ role: 'user', content: 'Reply with exactly: NapKey ready' }],
});
console.log(response.choices[0]?.message.content);`,
    },
    curl: {
      lang: 'bash',
      code: `curl ${base}/v1/messages \\
  -H "x-api-key: ${secret}" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "content-type: application/json" \\
  -d '{
    "model": "claude-sonnet-4.6",
    "max_tokens": 32,
    "messages": [{"role": "user", "content": "Reply with exactly: NapKey ready"}]
  }'`,
    },
  } satisfies Record<OnboardingTool, { lang: string; code: string }>;

  return snippets[tool];
}

function object(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === 'object' ? (value as Record<string, unknown>) : {};
}

export function parseOnboardingResponse(value: unknown): OnboardingResult {
  const response = object(value);
  const usage = object(response.usage);
  const content = Array.isArray(response.content) ? response.content : [];
  const firstText = content.map(object).find((item) => item.type === 'text');

  return {
    id: typeof response.id === 'string' ? response.id : '',
    model: typeof response.model === 'string' ? response.model : '',
    text: typeof firstText?.text === 'string' ? firstText.text : '',
    inputTokens: typeof usage.input_tokens === 'number' ? usage.input_tokens : 0,
    outputTokens: typeof usage.output_tokens === 'number' ? usage.output_tokens : 0,
  };
}

export function isValidOnboardingResponse(result: OnboardingResult) {
  return result.id.startsWith('msg_') && result.text.length > 0 && result.inputTokens > 0;
}
