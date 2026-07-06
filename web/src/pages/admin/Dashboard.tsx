import { useEffect, useState, useCallback, type ReactNode } from 'react';
import { api } from '@/lib/api';
import { formatBytes } from '@/lib/security';
import { Card, CardBody, CardHeader, CardTitle, Skeleton, Button } from '@/components/ui';

/* ============================== 类型定义 ============================== */

interface AdminStats {
  user_count: number;
  active_count: number;
  total_clips: number;
  total_storage: number;
  online_count: number;
}

/* ============================== 图标 ============================== */

function UsersIcon() {
  return (
    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
      <path d="M16 3.13a4 4 0 0 1 0 7.75" />
    </svg>
  );
}

function UserCheckIcon() {
  return (
    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <polyline points="16 11 18 13 22 9" />
    </svg>
  );
}

function WifiIcon() {
  return (
    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M5 13a10 10 0 0 1 14 0" />
      <path d="M8.5 16.5a5 5 0 0 1 7 0" />
      <path d="M2 8.82a15 15 0 0 1 20 0" />
      <line x1="12" y1="20" x2="12.01" y2="20" />
    </svg>
  );
}

function ClipboardIcon() {
  return (
    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <rect x="8" y="2" width="8" height="4" rx="1" ry="1" />
      <path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2" />
      <path d="M9 12l2 2 4-4" />
    </svg>
  );
}

function DatabaseIcon() {
  return (
    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <ellipse cx="12" cy="5" rx="9" ry="3" />
      <path d="M3 5v14a9 3 0 0 0 18 0V5" />
      <path d="M3 12a9 3 0 0 0 18 0" />
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

function AlertTriangleIcon({ size = 40 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
      <line x1="12" y1="9" x2="12" y2="13" />
      <line x1="12" y1="17" x2="12.01" y2="17" />
    </svg>
  );
}

function GaugeIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 14l4-4" />
      <path d="M3.34 19a10 10 0 1 1 17.32 0" />
    </svg>
  );
}

/* ============================== 辅助函数 ============================== */

function getErrorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

/* ============================== 统计卡片 ============================== */

interface StatCardProps {
  label: string;
  value: string | number;
  icon: ReactNode;
  accent: string; // 图标/数字强调色（CSS 变量值）
  index: number;
  loading?: boolean;
}

function StatCard({ label, value, icon, accent, index, loading }: StatCardProps) {
  return (
    // 外层 div 承载入场动画与错落延迟（Card 组件不接受 style）
    <div
      className="animate-slide-up"
      style={{ animationDelay: `${index * 60}ms` }}
    >
      <Card className="overflow-hidden">
        <CardBody className="p-5">
          <div className="flex items-start justify-between">
            <div className="min-w-0">
              <p className="text-sm text-[var(--text-muted)]">{label}</p>
              {loading ? (
                <Skeleton className="mt-2 h-8 w-24" />
              ) : (
                <p className="mt-2 text-2xl font-bold text-[var(--text-primary)] tabular-nums">
                  {value}
                </p>
              )}
            </div>
            <div
              className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl"
              style={{ backgroundColor: `color-mix(in srgb, ${accent} 14%, transparent)`, color: accent }}
            >
              {icon}
            </div>
          </div>
        </CardBody>
      </Card>
    </div>
  );
}

/* ============================== 主组件 ============================== */

const REFRESH_INTERVAL = 30_000; // 30 秒

export default function Dashboard() {
  const [stats, setStats] = useState<AdminStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);

  const fetchStats = useCallback(async (isRefresh = false) => {
    try {
      if (isRefresh) setRefreshing(true);
      setError(null);
      const data = await api.get<AdminStats>('/api/admin/stats');
      setStats(data);
      setLastUpdated(new Date());
    } catch (err) {
      setError(getErrorMessage(err, '数据加载失败'));
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    fetchStats();
    const interval = setInterval(() => fetchStats(true), REFRESH_INTERVAL);
    return () => clearInterval(interval);
  }, [fetchStats]);

  const handleRetry = () => {
    setLoading(true);
    fetchStats();
  };

  // 首次加载中
  if (loading && !stats && !error) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <Skeleton className="h-7 w-44" />
            <Skeleton className="mt-2 h-4 w-64" />
          </div>
          <Skeleton className="h-9 w-20" />
        </div>
        <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 xl:grid-cols-5">
          {[0, 1, 2, 3, 4].map((i) => (
            <Card key={i}>
              <CardBody className="p-5">
                <Skeleton className="h-4 w-20" />
                <Skeleton className="mt-3 h-8 w-24" />
              </CardBody>
            </Card>
          ))}
        </div>
      </div>
    );
  }

  // 首次加载失败
  if (error && !stats) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-xl font-bold text-[var(--text-primary)]">系统仪表盘</h1>
          <p className="mt-1 text-sm text-[var(--text-muted)]">系统运行概览</p>
        </div>
        <Card>
          <CardBody className="flex flex-col items-center justify-center gap-4 py-20">
            <div className="text-[var(--danger)]">
              <AlertTriangleIcon />
            </div>
            <div className="text-center">
              <p className="text-lg font-medium text-[var(--text-primary)]">数据加载失败</p>
              <p className="mt-1 text-sm text-[var(--text-secondary)]">{error}</p>
              <p className="mt-1 text-xs text-[var(--text-muted)]">
                管理后台统计接口可能尚未启用，请确认后端已实现 /api/admin/stats
              </p>
            </div>
            <Button variant="primary" onClick={handleRetry} loading={loading}>
              重试
            </Button>
          </CardBody>
        </Card>
      </div>
    );
  }

  const cards: StatCardProps[] = [
    { label: '用户总数', value: stats?.user_count ?? 0, icon: <UsersIcon />, accent: 'var(--brand-400)', index: 0 },
    { label: '活跃用户', value: stats?.active_count ?? 0, icon: <UserCheckIcon />, accent: 'var(--success)', index: 1 },
    { label: '在线连接', value: stats?.online_count ?? 0, icon: <WifiIcon />, accent: 'var(--info)', index: 2 },
    { label: '剪切板条目', value: stats?.total_clips ?? 0, icon: <ClipboardIcon />, accent: 'var(--warning)', index: 3 },
    { label: '存储用量', value: formatBytes(stats?.total_storage ?? 0), icon: <DatabaseIcon />, accent: 'var(--danger)', index: 4 },
  ];

  return (
    <div className="space-y-8 animate-fade-in">
      {/* 标题栏 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-bold text-[var(--text-primary)]">系统仪表盘</h1>
          <p className="mt-1 text-sm text-[var(--text-muted)]">
            {lastUpdated
              ? `最后更新 ${lastUpdated.toLocaleTimeString('zh-CN')} · 每 30 秒自动刷新`
              : '系统运行概览'}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => fetchStats(true)}
          loading={refreshing}
        >
          {!refreshing && <RefreshIcon />}
          刷新
        </Button>
      </div>

      {/* 部分错误提示（有数据但刷新失败） */}
      {error && stats && (
        <div className="flex items-start gap-2 rounded-lg border border-[var(--warning)]/40 bg-[var(--warning)]/10 px-4 py-3 text-sm text-[var(--warning)]">
          <AlertTriangleIcon size={16} />
          <span>数据刷新失败：{error}，当前显示的是上次缓存的数据。</span>
        </div>
      )}

      {/* 统计卡片网格 */}
      <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 xl:grid-cols-5">
        {cards.map((card) => (
          <StatCard key={card.label} {...card} loading={refreshing} />
        ))}
      </div>

      {/* 辅助信息 */}
      <Card className="animate-slide-up">
        <CardHeader>
          <div className="flex items-center gap-2">
            <span className="text-[var(--brand-400)]"><GaugeIcon /></span>
            <CardTitle>系统信息</CardTitle>
          </div>
        </CardHeader>
        <CardBody>
          <div className="grid grid-cols-2 gap-4 text-sm md:grid-cols-4">
            <div>
              <span className="text-[var(--text-muted)]">自动刷新间隔</span>
              <p className="mt-1 font-medium text-[var(--text-primary)]">30 秒</p>
            </div>
            <div>
              <span className="text-[var(--text-muted)]">接口地址</span>
              <p className="mt-1 font-mono text-xs text-[var(--text-primary)]">/api/admin/stats</p>
            </div>
            <div>
              <span className="text-[var(--text-muted)]">数据状态</span>
              <p className="mt-1">
                {error ? (
                  <span className="font-medium text-[var(--warning)]">部分异常</span>
                ) : (
                  <span className="inline-flex items-center gap-1 font-medium text-[var(--success)]">
                    <span className="h-1.5 w-1.5 rounded-full bg-[var(--success)]" />
                    正常
                  </span>
                )}
              </p>
            </div>
            <div>
              <span className="text-[var(--text-muted)]">最后更新</span>
              <p className="mt-1 font-medium text-[var(--text-primary)]">
                {lastUpdated ? lastUpdated.toLocaleString('zh-CN') : '—'}
              </p>
            </div>
          </div>
        </CardBody>
      </Card>
    </div>
  );
}

