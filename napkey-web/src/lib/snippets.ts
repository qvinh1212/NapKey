import { defaultModel } from './model-catalog';
import { site } from './site';

export type Snippet = {
  key: 'claudeCode' | 'anthropic' | 'openai' | 'curl';
  lang: string;
  code: string;
};

const base = site.apiBaseUrl;

export const snippets: readonly Snippet[] = [
  {
    key: 'claudeCode',
    lang: 'bash',
    code: `export ANTHROPIC_BASE_URL="${base}"
export ANTHROPIC_AUTH_TOKEN="nk_live_..."

claude`,
  },
  {
    key: 'anthropic',
    lang: 'python',
    code: `from anthropic import Anthropic

client = Anthropic(
    base_url="${base}",
    api_key="nk_live_...",
)

message = client.messages.create(
    model="${defaultModel}",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Chao Claude"}],
)
print(message.content[0].text)`,
  },
  {
    key: 'openai',
    lang: 'typescript',
    code: `import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: '${base}/v1',
  apiKey: process.env.NAPKEY_API_KEY,
});

const res = await client.chat.completions.create({
  model: '${defaultModel}',
  messages: [{ role: 'user', content: 'Chao Claude' }],
  stream: true,
});

for await (const chunk of res) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? '');
}`,
  },
  {
    key: 'curl',
    lang: 'bash',
    code: `curl ${base}/v1/messages \\
  -H "x-api-key: $NAPKEY_API_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "content-type: application/json" \\
  -d '{
    "model": "${defaultModel}",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Chao Claude"}]
  }'`,
  },
];
