/** 对应 agent/route/sessions/route.go 的 /api/sessions 客户端。 */

const sessionsPath = '/api/sessions';

export type Role = 'system' | 'user' | 'assistant';

export type ToolInvocationState =
  | 'input-streaming'
  | 'input-available'
  | 'approval-requested'
  | 'approval-responded'
  | 'output-available'
  | 'output-error'
  | 'output-denied';

export type ProviderMetadata = Record<string, unknown>;

export type ToolApproval = {
  id?: string;
  approved?: boolean;
  reason?: string;
  isAutomatic?: boolean;
  signature?: string;
};

type ToolFields = {
  toolCallId: string;
  toolName: string;
  state: ToolInvocationState;
  input?: unknown;
  output?: unknown;
  errorText?: string;
  providerExecuted?: boolean;
  approval?: ToolApproval;
  callProviderMetadata?: ProviderMetadata;
  resultProviderMetadata?: ProviderMetadata;
};

export type UIPart =
  | { type: 'text'; text: string; state?: string; providerMetadata?: ProviderMetadata }
  | { type: 'reasoning'; id?: string; text: string; state?: string; providerMetadata?: ProviderMetadata }
  | ({ type: `tool-${string}` } & ToolFields)
  | ({ type: 'dynamic-tool' } & ToolFields)
  | {
      type: 'file';
      mediaType: string;
      url: string;
      filename?: string;
      providerReference?: Record<string, string>;
      providerMetadata?: ProviderMetadata;
    }
  | { type: 'reasoning-file'; mediaType: string; url: string; providerMetadata?: ProviderMetadata }
  | { type: 'source-url'; sourceId: string; url: string; title?: string; providerMetadata?: ProviderMetadata }
  | {
      type: 'source-document';
      sourceId: string;
      mediaType: string;
      title?: string;
      filename?: string;
      providerMetadata?: ProviderMetadata;
    }
  | { type: `data-${string}`; id?: string; data: unknown }
  | { type: 'custom'; kind: string; providerMetadata?: ProviderMetadata }
  | { type: 'step-start' };

/** 与 aisdk.UIMessage 的 JSON 形状一致。 */
export type UIMessage = {
  id: string;
  role: Role;
  parts: UIPart[];
  metadata?: unknown;
};

export type SessionMetadata = {
  id: string;
  title: string;
  createdAt: string;
  updatedAt: string;
  messageCount: number;
  cwd: string;
};

export type Session = {
  metadata: SessionMetadata;
  messages: UIMessage[];
  /** 保存时的系统提示词快照，用于调试（运行时由 agent 每轮重新构建）。 */
  systemPrompt?: string[];
};

export type SaveSessionInput = {
  /** 为空时服务端生成 UUIDv7；传入时覆盖保存。 */
  sessionId?: string;
  messages: UIMessage[];
};

export type SaveSessionOutput = {
  sessionId: string;
  systemPrompt: string[];
};

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

async function request(path: string, init?: RequestInit): Promise<Response> {
  const headers = new Headers(init?.headers);
  if (!headers.has('Accept')) {
    headers.set('Accept', 'application/json');
  }
  const res = await fetch(path, { ...init, headers });
  if (!res.ok) {
    throw await toApiError(res);
  }
  return res;
}

async function toApiError(res: Response): Promise<ApiError> {
  const text = await res.text();
  let message = text || res.statusText;
  try {
    const body = JSON.parse(text) as { message?: unknown };
    if (typeof body.message === 'string' && body.message !== '') {
      message = body.message;
    }
  } catch {
    // 404 是纯文本 "session not found"。
  }
  return new ApiError(res.status, message);
}

/** GET /api/sessions。目录不存在时服务端返回 null，这里归一成空数组。 */
export async function listSessions(init?: RequestInit): Promise<SessionMetadata[]> {
  const res = await request(sessionsPath, init);
  const body = (await res.json()) as SessionMetadata[] | null;
  return body ?? [];
}

/** GET /api/sessions/:id。不存在时抛出 status 为 404 的 ApiError。 */
export async function getSession(id: string, init?: RequestInit): Promise<Session> {
  const res = await request(`${sessionsPath}/${encodeURIComponent(id)}`, init);
  return (await res.json()) as Session;
}

/** POST /api/sessions。 */
export async function saveSession(input: SaveSessionInput, init?: RequestInit): Promise<SaveSessionOutput> {
  const headers = new Headers(init?.headers);
  if (!headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  const res = await request(sessionsPath, {
    ...init,
    method: 'POST',
    headers,
    body: JSON.stringify(input),
  });
  return (await res.json()) as SaveSessionOutput;
}
