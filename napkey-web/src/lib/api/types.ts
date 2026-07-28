/**
 * Kieu du lieu tra ve tu napkey-core.
 *
 * Viet tay chu khong sinh tu dau: napkey-core dung `map[string]any` khi tra JSON
 * nen khong co OpenAPI de sinh. Doi lai, moi thay doi ben backend phai sua o day,
 * va do la chu y - typecheck se bat cho nao con doc field cu.
 *
 * Quy uoc tien: mo hinh tien cua NapKey la so nguyen micro-VND
 * (1 VND = 1.000.000 micros, DESIGN.md muc 5). Backend luon tra ca ba dang trong
 * `Money` de moi client lam tron giong nhau; UI hien `formatted`, con tinh toan
 * (neu can) thi dung `micros`. Khong bao gio cong don `vnd` - do la ban da lam tron.
 */

/** Mot so tien, kem san ban da lam tron va ban da dinh dang. */
export type Money = {
  /** Nguon su that. So nguyen micro-VND. */
  micros: number;
  /** Da lam tron XUONG ve dong nguyen. Chi de hien thi. */
  vnd: number;
  /** Da dinh dang san theo quy uoc VN, vi du "1.234 đ". */
  formatted: string;
};

export type Credits = {
  /** Integer microcredits; 1 credit = 1,000,000 microcredits. */
  micros: number;
  credits: number;
};

/** Token tach theo cach tinh tien. Bon loai co gia lech nhau hon mot bac. */
export type TokenBreakdown = {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  total: number;
};

export type SessionUser = {
  id: string;
  email: string;
  emailVerified: boolean;
  emailVerifiedAt: string | null;
  status: 'active' | 'suspended';
  isAdmin: boolean;
  createdAt: string;
};

export type SessionResponse = {
  user: SessionUser;
  permissions: string[];
  expiresAt: string;
};

export type OperationsSummary = {
  windowDays: number;
  revenue: Money;
  upstreamCostEstimate: Money;
  margin: Money;
  wallets: { driftCount: number; absoluteDrift: Money };
  payments: { unmatched: number; rejected: number; stuck: number };
  holds: { open: number; expired: number };
  keySync: { pending: number; failed: number };
  openAlerts: number;
  dataPlane: {
    healthy: boolean;
    error?: string;
    version?: string;
    accounts?: number;
    available?: number;
    totalRequests?: number;
    successRequests?: number;
    failedRequests?: number;
    totalTokens?: number;
    uptimeSeconds?: number;
    usageReporting?: { enabled: number; healthy: number; queued: number; sent: number; duplicate: number; dropped: number; pending: number };
  };
  generatedAt: string;
};

export type OperationsAlert = {
  id: string;
  type: string;
  severity: 'info' | 'warning' | 'critical';
  title: string;
  metadata: Record<string, unknown>;
  openedAt: string;
};

/** Trang thai dong bo key sang data plane. Key chi dung duoc khi da `synced`. */
export type KeySyncState = 'pending' | 'synced' | 'failed' | 'delete_pending';

export type KeyStatus = 'active' | 'revoked' | 'disabled' | 'provisioning';

export type ApiKey = {
  id: string;
  name: string;
  keyMasked: string;
  prefix: string;
  lastFour: string;
  testMode: boolean;
  enabled: boolean;
  status: KeyStatus;
  tokenLimit: number;
  creditLimit: number;
  tokensUsed: number;
  creditsUsed: number;
  requestsCount: number;
  createdAt: string;
  lastUsedAt: string | null;
  revokedAt: string | null;
  syncState: KeySyncState;
  syncError?: string;
};

export type KeyListResponse = { keys: ApiKey[] };

/**
 * Chi lan tao key moi tra ban tho, va chi mot lan duy nhat.
 *
 * Luu y ten field: `key` la CHUOI BAN THO, con metadata nam o `details`. Dat ten
 * nguoc voi truc giac nhung day la hop dong that cua napkey-core.
 */
export type CreateKeyResponse = {
  /** Ban tho day du. Khong luu o dau, khong lay lai duoc. */
  key: string;
  warning?: string;
  details: ApiKey;
};

/** Tong hop cho trang tong quan. */
export type UsageSummaryResponse = {
  usage: {
    totalTokens: number;
    totalRequests: number;
    activeKeys: number;
    totalCost: Money;
    /** Exact lifetime credits aggregated from the immutable usage ledger. */
    credits: Credits;
    /** Con lai tu Giai doan 2, la float64 cua kiro-go. Khong dung de tinh tien. */
    totalCredits: number;
  };
  last30Days: {
    requests: number;
    tokens: TokenBreakdown;
    cost: Money;
    credits: Credits;
    errorRequests: number;
    /** So request co output token la UOC LUONG, khong phai do duoc. */
    estimatedRequests: number;
    /** So request duoc phuc vu ma khong co gia tren so - can nguoi xu ly. */
    unpricedRequests: number;
  };
  billing: {
    mode: 'prepaid_wallet' | 'metered_no_wallet' | 'manual_quota';
    message: string;
  };
};

export type UsageDayBucket = {
  /** Dang YYYY-MM-DD, cat theo moc ngay gio Viet Nam. */
  day: string;
  requests: number;
  tokens: TokenBreakdown;
  cost: Money;
  credits: Credits;
};

export type UsageModelBucket = {
  model: string;
  requests: number;
  tokens: TokenBreakdown;
  cost: Money;
  credits: Credits;
};

export type UsageDetailResponse = {
  range: { from: string; to: string };
  totals: {
    requests: number;
    tokens: TokenBreakdown;
    cost: Money;
    credits: Credits;
    errorRequests: number;
    estimatedRequests: number;
    unpricedRequests: number;
  };
  daily: UsageDayBucket[];
  byModel: UsageModelBucket[];
};

export type UsageRecord = {
  id: number;
  requestId: string;
  model: string;
  tokens: TokenBreakdown;
  cost: Money;
  credits: Credits;
  /** Phuc vu roi nhung khong co gia - tinh 0 dong. */
  unpriced: boolean;
  /** Output token la uoc luong, khong phai do tu upstream. */
  estimated: boolean;
  status: 'success' | 'error' | 'cancelled';
  createdAt: string;
  latencyMs?: number;
  /** Vang khi key da bi xoa - so usage song lau hon key. */
  keyId?: string;
  keyName?: string;
  keyMasked?: string;
};

export type UsageRecordsResponse = {
  records: UsageRecord[];
  total: number;
  limit: number;
  offset: number;
};

export type WalletResponse = {
  wallet: {
    balance: Money;
    held: Money;
    available: Money;
    credits: {
      balance: Credits;
      held: Credits;
      available: Credits;
      vndPerCredit: 60;
    };
    currency: 'VND';
  };
};

export type TopupStatus = 'pending' | 'paid' | 'underpaid' | 'expired' | 'cancelled';

export type TopupOrderResponse = {
  order: {
    id: string;
    memoCode: string;
    status: TopupStatus;
    expectedAmount: Money;
    expectedCredits: Credits;
    receivedAmount: Money;
    expiresAt: string;
    paidAt: string | null;
    payment: {
      provider: 'payos';
      checkoutUrl: string;
      qrCode: string;
    };
  };
};

/** Ma loi backend tra ve, de UI phan nhanh thay vi doc chuoi tieng Anh. */
export type ApiErrorCode =
  | 'invalid_request'
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'conflict'
  | 'rate_limited'
  | 'internal_error'
  | 'email_unverified'
  | 'upstream_failure';

export type ApiErrorBody = {
  error: {
    code: ApiErrorCode;
    message: string;
    /** Loi theo tung field, dung cho form. */
    fields?: Record<string, string>;
  };
};
