import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react';
import { api, ApiClientError, tokenStorage } from './api';
import { wsClient } from './ws';
import type { AuthResponse, User } from '../types';

/**
 * Auth Context
 * - 管理当前用户状态与认证凭证
 * - 初始化时自动检查 localStorage 中的 token，尝试 /api/me 恢复会话
 * - 暴露 accessToken 供 WebSocket 鉴权与直接 fetch（如下载 blob）使用
 */

interface AuthContextValue {
  user: User | null;
  accessToken: string | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<User>;
  register: (username: string, password: string, inviteCode?: string) => Promise<User>;
  logout: () => Promise<void>;
  refreshUser: () => Promise<User | null>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [accessToken, setAccessToken] = useState<string | null>(
    () => tokenStorage.getAccessToken(),
  );
  const [loading, setLoading] = useState<boolean>(true);

  /** 从 /api/me 拉取最新用户信息 */
  const refreshUser = useCallback(async (): Promise<User | null> => {
    if (!tokenStorage.getAccessToken()) {
      setUser(null);
      setAccessToken(null);
      return null;
    }
    try {
      const u = await api.get<User>('/api/me');
      setUser(u);
      setAccessToken(tokenStorage.getAccessToken());
      return u;
    } catch (err) {
      // 仅明确的 401 视为会话失效；网络错误/5xx 时保留本地凭证
      // （服务器重启或断网不应销毁本地仍有效的 refresh token）
      if (err instanceof ApiClientError && err.status === 401) {
        tokenStorage.clear();
        setUser(null);
        setAccessToken(null);
      }
      return null;
    }
  }, []);

  // 初始化：尝试恢复会话
  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (tokenStorage.getAccessToken()) {
        try {
          const u = await api.get<User>('/api/me');
          if (!cancelled) {
            setUser(u);
            setAccessToken(tokenStorage.getAccessToken());
          }
        } catch (err) {
          // 与 refreshUser 一致：仅明确的 401 清除凭证，网络错误/5xx 保留
          if (err instanceof ApiClientError && err.status === 401) {
            tokenStorage.clear();
            if (!cancelled) setAccessToken(null);
          }
        }
      }
      if (!cancelled) setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const login = useCallback(async (username: string, password: string): Promise<User> => {
    const res = await api.post<AuthResponse>('/api/auth/login', { username, password });
    tokenStorage.setTokens(res.access_token, res.refresh_token);
    setUser(res.user);
    setAccessToken(res.access_token);
    return res.user;
  }, []);

  const register = useCallback(
    async (username: string, password: string, inviteCode?: string): Promise<User> => {
      const body: Record<string, string> = { username, password };
      if (inviteCode) body.invite_code = inviteCode;
      const res = await api.post<AuthResponse>('/api/auth/register', body);
      tokenStorage.setTokens(res.access_token, res.refresh_token);
      setUser(res.user);
      setAccessToken(res.access_token);
      return res.user;
    },
    [],
  );

  const logout = useCallback(async (): Promise<void> => {
    const refreshToken = tokenStorage.getRefreshToken();
    try {
      await api.post('/api/auth/logout', { refresh_token: refreshToken });
    } catch {
      // 网络错误也继续清理本地状态
    }
    wsClient.disconnect();
    tokenStorage.clear();
    setUser(null);
    setAccessToken(null);
  }, []);

  const value: AuthContextValue = {
    user,
    accessToken,
    loading,
    login,
    register,
    logout,
    refreshUser,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

/** useAuth hook：必须在 AuthProvider 内使用 */
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth 必须在 AuthProvider 内使用');
  }
  return ctx;
}
