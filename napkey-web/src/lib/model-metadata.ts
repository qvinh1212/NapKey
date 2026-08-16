import { MODEL_PRICES } from './pricing';

export type ModelCapability = 'coding' | 'fast' | 'thinking';

export interface ModelMetadata {
  id: string;
  name: string;
  family: string;
  ratio: number;
  pricePerMillion: number;
  capabilities: readonly ModelCapability[];
  tags: readonly string[];
  contextWindow: string;
  recommendedFor: string;
}

export const ENRICHED_MODELS: readonly ModelMetadata[] = [
  {
    id: 'claude-sonnet-5',
    name: 'Claude Sonnet 5',
    family: 'Anthropic Messages',
    ratio: 0.5,
    pricePerMillion: MODEL_PRICES['claude-sonnet-5'] ?? 1_500,
    capabilities: ['coding', 'fast'],
    tags: ['200K Context', 'Coding Supercharged', '85+ tps'],
    contextWindow: '200K tokens',
    recommendedFor: 'Tối ưu cho Claude Code, Cursor, refactoring và lập trình hàng ngày.',
  },
  {
    id: 'gpt-5.6-luna',
    name: 'GPT 5.6 Luna',
    family: 'OpenAI Chat',
    ratio: 0.5,
    pricePerMillion: MODEL_PRICES['gpt-5.6-luna'] ?? 1_500,
    capabilities: ['fast', 'coding'],
    tags: ['128K Context', 'Siêu tốc', '120+ tps'],
    contextWindow: '128K tokens',
    recommendedFor: 'Tác vụ phản hồi nhanh, chatbot, tóm tắt và xử lý dữ liệu song song.',
  },
  {
    id: 'claude-opus-4.7',
    name: 'Claude Opus 4.7',
    family: 'Anthropic Messages',
    ratio: 1.0,
    pricePerMillion: MODEL_PRICES['claude-opus-4.7'] ?? 3_000,
    capabilities: ['coding', 'thinking'],
    tags: ['200K Context', 'Deep Reasoning', 'Tool Use'],
    contextWindow: '200K tokens',
    recommendedFor: 'Giải quyết logic phức tạp, kiến trúc hệ thống và debug sâu.',
  },
  {
    id: 'claude-opus-4.8',
    name: 'Claude Opus 4.8',
    family: 'Anthropic Messages',
    ratio: 1.0,
    pricePerMillion: MODEL_PRICES['claude-opus-4.8'] ?? 3_000,
    capabilities: ['coding', 'thinking'],
    tags: ['200K Context', 'Thinking Mode', 'Agentic Loop'],
    contextWindow: '200K tokens',
    recommendedFor: 'Các workflow agent tự động dài hơi và suy luận nhiều bước.',
  },
  {
    id: 'gpt-5.6-terra',
    name: 'GPT 5.6 Terra',
    family: 'OpenAI Chat',
    ratio: 1.2,
    pricePerMillion: MODEL_PRICES['gpt-5.6-terra'] ?? 3_600,
    capabilities: ['coding', 'thinking'],
    tags: ['256K Context', 'High Precision'],
    contextWindow: '256K tokens',
    recommendedFor: 'Xử lý file tài liệu lớn và trích xuất logic chuẩn xác.',
  },
  {
    id: 'claude-opus-5',
    name: 'Claude Opus 5',
    family: 'Anthropic Messages',
    ratio: 1.5,
    pricePerMillion: MODEL_PRICES['claude-opus-5'] ?? 4_500,
    capabilities: ['thinking', 'coding'],
    tags: ['200K Context', 'Extended Reasoning'],
    contextWindow: '200K tokens',
    recommendedFor: 'Suy luận chuyên gia, toán học và bài toán thuật toán nâng cao.',
  },
  {
    id: 'gpt-5.6-sol',
    name: 'GPT 5.6 Sol',
    family: 'OpenAI Chat',
    ratio: 2.0,
    pricePerMillion: MODEL_PRICES['gpt-5.6-sol'] ?? 6_000,
    capabilities: ['thinking'],
    tags: ['384K Context', 'Expert Tier'],
    contextWindow: '384K tokens',
    recommendedFor: 'Tổng hợp phân tích đa nguồn với độ chi tiết tối đa.',
  },
  {
    id: 'claude-fable-5',
    name: 'Claude Fable 5',
    family: 'Anthropic Messages',
    ratio: 3.3,
    pricePerMillion: MODEL_PRICES['claude-fable-5'] ?? 10_000,
    capabilities: ['thinking'],
    tags: ['Flagship Reasoning', 'Anchor Tier'],
    contextWindow: '200K tokens',
    recommendedFor: 'Mô hình nghiên cứu đầu bảng với năng lực suy diễn đỉnh cao.',
  },
] as const;
