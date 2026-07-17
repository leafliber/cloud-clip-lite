import type { ApiError } from '../types';

/**
 * API 客户端
 * - 基于 fetch 封装，支持 JWT 自动注入与刷新
 * - Token 存储于 localStorage（access_token + refresh_token）
 * - 请求拦截：自动添加 Authorization: Bearer 头
 * - 响应拦截：401 时自动尝试 refresh，失败则清除凭证并跳转登录
 * - 安全防护：所有 URL 参数通过 URLSearchParams 编码，防止注入
 */

const ACCESS_TOKEN_KEY = 'cc_access_token';
const REFRESH_TOKEN_KEY = 'cc_refresh_token';
const LOGIN_PATH = '/login';

/** Token 存储工具 */
export const tokenStorage = {
  getAccessToken(): string | null {
    try {
      return localStorage.getItem(ACCESS_TOKEN_KEY);
    } catch {
      return null;
    }
  },
  getRefreshToken(): string | null {
    try {
      return localStorage.getItem(REFRESH_TOKEN_KEY);
    } catch {
      return null;
    }
  },
  setTokens(accessToken: string, refreshToken: string): void {
    try {
      localStorage.setItem(ACCESS_TOKEN_KEY, accessToken);
      localStorage.setItem(REFRESH_TOKEN_KEY, refreshToken);
    } catch {
      // localStorage 不可用（隐私模式等），忽略
    }
  },
  clear(): void {
    try {
      localStorage.removeItem(ACCESS_TOKEN_KEY);
      localStorage.removeItem(REFRESH_TOKEN_KEY);
    } catch {
      // ignore
    }
  },
};

/** API 错误，携带 HTTP 状态码与后端错误码 */
export class ApiClientError extends Error {
  status: number;
  code?: string;
  extra?: Record<string, any>;
  body?: any;

  constructor(message: string, status: number, code?: string, body?: any) {
    super(message);
    this.name = 'ApiClientError';
    this.status = status;
    this.code = code;
    this.body = body;
    this.extra = body?.error?.extra;
  }
}

/** 构造查询字符串，所有参数经 URLSearchParams 编码 */
function buildQuery(params?: Record<string, any>): string {
  if (!params) return '';
  const sp = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue;
    sp.append(key, String(value));
  }
  const str = sp.toString();
  return str ? `?${str}` : '';
}

/** 从非 2xx 响应中解析错误 */
async function parseError(res: Response): Promise<ApiClientError> {
  let body: any = null;
  try {
    body = await res.json();
  } catch {
    // 响应非 JSON
  }
  const err = body?.error as ApiError['error'] | undefined;
  const message = err?.message || `请求失败 (${res.status})`;
  return new ApiClientError(message, res.status, err?.code, body);
}

interface RequestOptions {
  params?: Record<string, any>;
  body?: any;
  isForm?: boolean;
}

class ApiClient {
  /** 并发 refresh 去重：同一时刻只允许一个 refresh 请求 */
  private refreshPromise: Promise<boolean> | null = null;

  /** 核心请求方法 */
  private async request<T>(
    method: string,
    path: string,
    opts: RequestOptions,
  ): Promise<T> {
    const url = `${path}${buildQuery(opts.params)}`;
    const { headers, body } = this.prepareBody(opts);

    const token = tokenStorage.getAccessToken();
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }

    let res = await fetch(url, { method, headers, body });

    // 401 → 尝试刷新一次令牌后重试
    if (res.status === 401 && !this.isAuthEndpoint(path)) {
      const refreshed = await this.tryRefresh();
      if (refreshed) {
        const newToken = tokenStorage.getAccessToken();
        if (newToken) headers['Authorization'] = `Bearer ${newToken}`;
        // FormData / 字符串 body 均可重用
        res = await fetch(url, { method, headers, body });
      }
    }

    if (!res.ok) {
      throw await parseError(res);
    }

    // 204 或空响应
    if (res.status === 204) return undefined as T;
    const text = await res.text();
    if (!text) return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch {
      return text as unknown as T;
    }
  }

  /** 准备请求体与 headers */
  private prepareBody(opts: RequestOptions): {
    headers: Record<string, string>;
    body: BodyInit | undefined;
  } {
    const headers: Record<string, string> = {};
    let body: BodyInit | undefined;

    if (opts.isForm && opts.body instanceof FormData) {
      // multipart：不设置 Content-Type，浏览器自动追加 boundary
      body = opts.body;
    } else if (opts.body !== undefined && opts.body !== null) {
      headers['Content-Type'] = 'application/json';
      body = JSON.stringify(opts.body);
    }

    return { headers, body };
  }

  /** 判断是否为认证端点（避免对 login/register/refresh 自身触发刷新） */
  private isAuthEndpoint(path: string): boolean {
    return path.startsWith('/api/auth/');
  }

  /** 尝试刷新令牌，返回是否成功。并发请求共用同一刷新 Promise。 */
  private tryRefresh(): Promise<boolean> {
    if (this.refreshPromise) return this.refreshPromise;
    this.refreshPromise = this.doRefresh().finally(() => {
      this.refreshPromise = null;
    });
    return this.refreshPromise;
  }

  private async doRefresh(): Promise<boolean> {
    let refreshToken = tokenStorage.getRefreshToken();
    if (!refreshToken) {
      this.handleAuthFailure();
      return false;
    }
    // 最多尝试两次：第二次仅在 refresh token 被其他标签页轮转后重试
    for (let attempt = 0; attempt < 2; attempt++) {
      let res: Response;
      try {
        res = await fetch('/api/auth/refresh', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ refresh_token: refreshToken }),
        });
      } catch {
        // 网络错误/服务器不可达：保留本地凭证，不当作会话失效
        return false;
      }
      if (res.ok) {
        try {
          const data = await res.json();
          if (data.access_token && data.refresh_token) {
            tokenStorage.setTokens(data.access_token, data.refresh_token);
            return true;
          }
        } catch {
          // 响应解析失败：按刷新失败处理，但保留凭证
        }
        return false;
      }
      if (res.status === 401) {
        // 多标签页并发 refresh：后端轮转吊销旧 token 后，其他标签页可能已写入新
        // token。若 localStorage 中的 refresh token 与本次使用的不一样，用它重试一次。
        const latest = tokenStorage.getRefreshToken();
        if (attempt === 0 && latest && latest !== refreshToken) {
          refreshToken = latest;
          continue;
        }
        // 明确的 401：会话确实失效
        this.handleAuthFailure();
        return false;
      }
      // 5xx 等其他错误：保留凭证，下次再试
      return false;
    }
    return false;
  }

  /** 鉴权失败：清除凭证并跳转登录页 */
  private handleAuthFailure(): void {
    tokenStorage.clear();
    if (typeof window !== 'undefined' && window.location.pathname !== LOGIN_PATH) {
      window.location.href = LOGIN_PATH;
    }
  }

  get<T>(path: string, params?: Record<string, any>): Promise<T> {
    return this.request<T>('GET', path, { params });
  }

  /**
   * 拉取二进制内容（Blob），与 request 一样内置 401 → refresh → 重试。
   * 用于图片缩略图、文件下载等二进制端点。
   */
  async getBlob(path: string, params?: Record<string, any>): Promise<Blob> {
    const url = `${path}${buildQuery(params)}`;
    const doFetch = (): Promise<Response> => {
      const headers: Record<string, string> = {};
      const token = tokenStorage.getAccessToken();
      if (token) headers['Authorization'] = `Bearer ${token}`;
      return fetch(url, { method: 'GET', headers });
    };

    let res = await doFetch();
    if (res.status === 401 && !this.isAuthEndpoint(path)) {
      const refreshed = await this.tryRefresh();
      if (refreshed) res = await doFetch();
    }
    if (!res.ok) {
      throw await parseError(res);
    }
    return res.blob();
  }

  post<T>(path: string, body?: any, isForm?: boolean): Promise<T> {
    return this.request<T>('POST', path, { body, isForm });
  }

  patch<T>(path: string, body?: any): Promise<T> {
    return this.request<T>('PATCH', path, { body });
  }

  del<T>(path: string): Promise<T> {
    return this.request<T>('DELETE', path, {});
  }
}

export const api = new ApiClient();
