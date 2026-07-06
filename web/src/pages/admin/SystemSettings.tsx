import { useEffect, useState, useCallback, type ReactNode } from 'react';
import { api } from '@/lib/api';
import { sanitizeText, formatBytes, formatTime } from '@/lib/security';
import {
  Card,
  CardHeader,
  CardTitle,
  CardBody,
  Skeleton,
  Button,
  Badge,
} from '@/components/ui';

/* ============================== 类型定义 ============================== */

interface SystemConfig {
  allow_register?: string;
  default_max_item_size?: number;
  default_quota_bytes?: number;
  default_retention_days?: number;
  access_ttl?: string;
  refresh_ttl?: string;
  rate_limit_per_minute?: number;
  blob_store?: string;
}

interface MeResponse {
  id: number;
  username: string;
  email?: string;
  role: string;
  status: string;
  max_item_size: number;
  quota_bytes: number;
  retention_days: number;
  created_at: string;
}

/* ============================== 图标 ============================== */

function SettingsIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="3" />
      <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
    </svg>
  );
}

function AlertTriangleIcon({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
      <line x1="12" y1="9" x2="12" y2="13" />
      <line x1="12" y1="17" x2="12.01" y2="17" />
    </svg>
  );
}

function InfoIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="10" />
      <line x1="12" y1="16" x2="12" y2="12" />
      <line x1="12" y1="8" x2="12.01" y2="8" />
    </svg>
  );
}

function RefreshIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" />
      <path d="M21 3v5h-5" />
      <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" />
      <path d="M3 21v-5h5" />
    </svg>
  );
}

function ServerIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <rect x="2" y="2" width="20" height="8" rx="2" ry="2" />
      <rect x="2" y="14" width="20" height="8" rx="2" ry="2" />
      <line x1="6" y1="6" x2="6.01" y2="6" />
      <line x1="6" y1="18" x2="6.01" y2="18" />
    </svg>
  );
}

function TerminalIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <polyline points="4 17 10 11 4 5" />
      <line x1="12" y1="19" x2="20" y2="19" />
    </svg>
  );
}

/* ============================== 辅助函数 ============================== */

function getErrorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

function registerModeLabel(mode?: string): string {
  switch (mode) {
    case 'open':
      return '开放注册';
    case 'invite':
      return '邀请码注册';
    case 'closed':
      return '仅管理员创建';
    default:
      return '未知';
  }
}

function registerModeVariant(mode?: string): 'success' | 'warning' | 'danger' | 'default' {
  switch (mode) {
    case 'open':
      return 'success';
    case 'invite':
      return 'warning';
    case 'closed':
      return 'danger';
    default:
      return 'default';
  }
}

function blobStoreLabel(s?: string): string {
  switch (s) {
    case 's3':
      return 'S3 兼容';
    case 'local':
      return '本地存储';
    default:
      return s ?? '—';
  }
}

/* ============================== 配置项组件 ============================== */

/** 只读配置卡片：以网格方式展示键值，比逐行更紧凑 */
function ReadOnlyConfigGrid({ items }: { items: { label: string; value: ReactNode; hint?: string }[] }) {
  return (
    <div className="grid grid-cols-1 gap-px overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[var(--border-subtle)] sm:grid-cols-2">
      {items.map((it) => (
        <div key={it.label} className="bg-[var(--bg-surface)] p-4">
          <p className="text-xs text-[var(--text-muted)]">{it.label}</p>
          <div className="mt-1 text-sm font-medium text-[var(--text-primary)]">{it.value}</div>
          {it.hint && <p className="mt-1 text-xs text-[var(--text-muted)]">{it.hint}</p>}
        </div>
      ))}
    </div>
  );
}

/* ============================== 主组件 ============================== */

export default function SystemSettings() {
  const [systemConfig, setSystemConfig] = useState<SystemConfig | null>(null);
  const [meData, setMeData] = useState<MeResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [systemError, setSystemError] = useState<string | null>(null);
  const [meError, setMeError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const fetchSystemConfig = useCallback(async (isRefresh = false) => {
    try {
      if (isRefresh) setRefreshing(true);
      setSystemError(null);
      // 尝试获取系统级配置（后端可能尚未实现此接口，会返回 404）
      const data = await api.get<SystemConfig>('/api/admin/settings');
      setSystemConfig(data);
    } catch (err) {
      setSystemError(getErrorMessage(err, '系统配置接口不可用'));
    } finally {
      if (isRefresh) setRefreshing(false);
    }
  }, []);

  const fetchMe = useCallback(async () => {
    try {
      setMeError(null);
      const data = await api.get<MeResponse>('/api/me');
      setMeData(data);
    } catch (err) {
      setMeError(getErrorMessage(err, '获取当前用户信息失败'));
    }
  }, []);

  useEffect(() => {
    Promise.all([fetchSystemConfig(), fetchMe()]).finally(() => {
      setLoading(false);
    });
  }, [fetchSystemConfig, fetchMe]);

  const handleRefresh = () => {
    fetchSystemConfig(true);
    fetchMe();
  };

  if (loading) {
    return (
      <div className="space-y-6">
        <div>
          <Skeleton className="h-7 w-40" />
          <Skeleton className="mt-2 h-4 w-56" />
        </div>
        <Skeleton className="h-16 w-full" />
        {[0, 1].map((i) => (
          <Card key={i}>
            <CardHeader>
              <Skeleton className="h-5 w-40" />
            </CardHeader>
            <CardBody>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                {[0, 1, 2, 3].map((j) => (
                  <Skeleton key={j} className="h-16 w-full" />
                ))}
              </div>
            </CardBody>
          </Card>
        ))}
      </div>
    );
  }

  // 环境变量参考表数据
  const envRows: { name: string; desc: string; def: string }[] = [
    { name: 'ALLOW_REGISTER', desc: '注册模式', def: 'closed' },
    { name: 'DEFAULT_MAX_ITEM_SIZE', desc: '默认单条上限（字节）', def: '10485760 (10MB)' },
    { name: 'DEFAULT_QUOTA_BYTES', desc: '默认总配额（字节）', def: '1073741824 (1GB)' },
    { name: 'DEFAULT_RETENTION_DAYS', desc: '默认保留天数', def: '30' },
    { name: 'BLOB_STORE', desc: '对象存储方式', def: 'local' },
    { name: 'JWT_SECRET', desc: 'JWT 签名密钥（必填）', def: '—' },
    { name: 'ACCESS_TTL', desc: 'Access Token 有效期', def: '15m' },
    { name: 'REFRESH_TTL', desc: 'Refresh Token 有效期', def: '720h' },
    { name: 'RATE_LIMIT_PER_MINUTE', desc: '每分钟请求限流', def: '60' },
  ];

  return (
    <div className="space-y-8 animate-fade-in">
      {/* 标题栏 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div
            className="flex h-11 w-11 items-center justify-center rounded-xl text-white shadow-sm"
            style={{ background: 'linear-gradient(135deg, var(--brand-500), var(--brand-700))' }}
          >
            <SettingsIcon />
          </div>
          <div>
            <h1 className="text-xl font-bold text-[var(--text-primary)]">系统设置</h1>
            <p className="mt-0.5 text-sm text-[var(--text-muted)]">查看系统级配置参数</p>
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={handleRefresh} loading={refreshing}>
          {!refreshing && <RefreshIcon />}
          刷新
        </Button>
      </div>

      {/* 信息提示卡片 */}
      <div className="flex items-start gap-3 rounded-xl border border-[var(--brand-500)]/30 bg-[var(--brand-500)]/10 px-4 py-3.5 text-sm text-[var(--brand-400)]">
        <InfoIcon />
        <div>
          <p className="font-medium text-[var(--text-primary)]">系统级配置通过环境变量设置</p>
          <p className="mt-1 text-xs text-[var(--text-secondary)]">
            修改环境变量后需要重启服务才能生效。可配置项包括：注册模式、默认配额、默认保留天数、JWT 密钥、Argon2 参数等。
          </p>
        </div>
      </div>

      {/* 系统配置（只读） */}
      <Card className="animate-slide-up">
        <CardHeader>
          <div className="flex items-center gap-2">
            <span className="text-[var(--brand-400)]"><ServerIcon /></span>
            <CardTitle>系统配置（只读）</CardTitle>
          </div>
        </CardHeader>
        <CardBody>
          {systemError ? (
            <div className="flex flex-col items-center justify-center gap-3 py-12 text-center">
              <div className="text-[var(--warning)]">
                <AlertTriangleIcon size={36} />
              </div>
              <div>
                <p className="text-sm font-medium text-[var(--text-primary)]">系统配置接口暂不可用</p>
                <p className="mt-1 text-xs text-[var(--text-muted)]">
                  后端可能尚未实现 /api/admin/settings 接口（可能返回 404）
                </p>
                <p className="mt-1 text-xs text-[var(--text-muted)]">
                  系统配置通过环境变量管理，当前无法从 API 获取。
                  <br />
                  请检查 .env 文件或容器环境变量配置。
                </p>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => fetchSystemConfig(true)}
                loading={refreshing}
              >
                <RefreshIcon />
                重新获取
              </Button>
            </div>
          ) : systemConfig ? (
            <ReadOnlyConfigGrid
              items={[
                {
                  label: '注册模式',
                  value: (
                    <Badge variant={registerModeVariant(systemConfig.allow_register)}>
                      {registerModeLabel(systemConfig.allow_register)}
                    </Badge>
                  ),
                  hint: '控制新用户注册方式',
                },
                {
                  label: '默认单条上限',
                  value: formatBytes(systemConfig.default_max_item_size ?? 0),
                  hint: '新用户单条剪切板大小上限',
                },
                {
                  label: '默认总配额',
                  value: formatBytes(systemConfig.default_quota_bytes ?? 0),
                  hint: '新用户存储总配额',
                },
                {
                  label: '默认保留天数',
                  value: `${systemConfig.default_retention_days ?? '—'} 天`,
                  hint: '新用户历史保留天数',
                },
                {
                  label: '对象存储方式',
                  value: blobStoreLabel(systemConfig.blob_store),
                  hint: '二进制文件存储后端',
                },
                {
                  label: '限流（每分钟）',
                  value: `${systemConfig.rate_limit_per_minute ?? '—'} 次/分`,
                  hint: '单用户每分钟请求上限',
                },
                {
                  label: 'Access Token 有效期',
                  value: systemConfig.access_ttl ?? '—',
                  hint: 'JWT 访问令牌有效期',
                },
                {
                  label: 'Refresh Token 有效期',
                  value: systemConfig.refresh_ttl ?? '—',
                  hint: 'JWT 刷新令牌有效期',
                },
              ]}
            />
          ) : (
            <div className="py-10 text-center text-sm text-[var(--text-muted)]">
              暂无系统配置数据
            </div>
          )}
        </CardBody>
      </Card>

      {/* 当前管理员配置参考 */}
      <Card className="animate-slide-up">
        <CardHeader>
          <div className="flex items-center gap-2">
            <span className="text-[var(--brand-400)]"><SettingsIcon /></span>
            <CardTitle>当前管理员配置参考</CardTitle>
          </div>
        </CardHeader>
        <CardBody>
          {meError ? (
            <div className="flex items-start gap-2 rounded-lg border border-[var(--warning)]/40 bg-[var(--warning)]/10 px-4 py-3 text-sm text-[var(--warning)]">
              <AlertTriangleIcon />
              <span>获取当前用户信息失败：{meError}</span>
            </div>
          ) : meData ? (
            <div>
              <div className="mb-5 flex items-center gap-3">
                <div
                  className="flex h-12 w-12 items-center justify-center rounded-full text-base font-semibold text-white"
                  style={{ background: 'linear-gradient(135deg, var(--brand-400), var(--brand-600))' }}
                >
                  {sanitizeText(meData.username).slice(0, 2).toUpperCase()}
                </div>
                <div>
                  <p className="font-semibold text-[var(--text-primary)]">{sanitizeText(meData.username)}</p>
                  <div className="mt-1 flex items-center gap-2">
                    <Badge variant={meData.role === 'admin' ? 'info' : 'default'}>
                      {meData.role === 'admin' ? '管理员' : '普通用户'}
                    </Badge>
                    <Badge variant={meData.status === 'active' ? 'success' : 'danger'}>
                      {meData.status === 'active' ? '正常' : '已禁用'}
                    </Badge>
                  </div>
                </div>
              </div>

              <ReadOnlyConfigGrid
                items={[
                  { label: '用户 ID', value: meData.id },
                  { label: '邮箱', value: meData.email ? sanitizeText(meData.email) : '未设置' },
                  { label: '单条上限', value: formatBytes(meData.max_item_size) },
                  { label: '总配额', value: formatBytes(meData.quota_bytes) },
                  { label: '保留天数', value: `${meData.retention_days} 天` },
                  { label: '账号创建时间', value: formatTime(meData.created_at) },
                ]}
              />
            </div>
          ) : (
            <div className="py-10 text-center text-sm text-[var(--text-muted)]">
              暂无用户数据
            </div>
          )}
        </CardBody>
      </Card>

      {/* 环境变量参考 */}
      <Card className="animate-slide-up">
        <CardHeader>
          <div className="flex items-center gap-2">
            <span className="text-[var(--brand-400)]"><TerminalIcon /></span>
            <CardTitle>环境变量参考</CardTitle>
          </div>
        </CardHeader>
        <CardBody>
          <p className="mb-4 text-sm text-[var(--text-secondary)]">
            以下环境变量控制系统的核心行为，请在服务启动前通过 .env 文件或容器环境变量设置：
          </p>
          {/* 桌面表格 */}
          <div className="hidden overflow-x-auto md:block">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-[var(--border-subtle)] text-left text-xs text-[var(--text-muted)]">
                  <th className="px-3 py-2.5 font-medium">变量名</th>
                  <th className="px-3 py-2.5 font-medium">说明</th>
                  <th className="px-3 py-2.5 font-medium">默认值</th>
                </tr>
              </thead>
              <tbody>
                {envRows.map((row) => (
                  <tr
                    key={row.name}
                    className="border-b border-[var(--border-subtle)] transition-colors last:border-0 hover:bg-[var(--bg-hover)]"
                  >
                    <td className="px-3 py-2.5 font-mono text-xs text-[var(--brand-400)]">{row.name}</td>
                    <td className="px-3 py-2.5 text-[var(--text-secondary)]">{row.desc}</td>
                    <td className="px-3 py-2.5 font-mono text-xs text-[var(--text-muted)]">{row.def}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {/* 移动端卡片 */}
          <div className="space-y-2 md:hidden">
            {envRows.map((row) => (
              <div
                key={row.name}
                className="rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] p-3"
              >
                <p className="font-mono text-xs text-[var(--brand-400)]">{row.name}</p>
                <p className="mt-1 text-sm text-[var(--text-secondary)]">{row.desc}</p>
                <p className="mt-1 font-mono text-xs text-[var(--text-muted)]">默认：{row.def}</p>
              </div>
            ))}
          </div>
        </CardBody>
      </Card>
    </div>
  );
}
