import { useCallback, useEffect, useState } from 'react';
import { Navigate, useNavigate } from 'react-router-dom';
import { useAuth } from '@/lib/auth';
import { api } from '@/lib/api';
import { formatBytes, sanitizeText } from '@/lib/security';
import {
  Button,
  Input,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  Skeleton,
  useToast,
} from '@/components/ui';
import type { User, ClipItem } from '@/types';

/* ============================== 图标 ============================== */

function KeyIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />
    </svg>
  );
}

function DatabaseIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <ellipse cx="12" cy="5" rx="9" ry="3" />
      <path d="M3 5v14a9 3 0 0 0 18 0V5" />
      <path d="M3 12a9 3 0 0 0 18 0" />
    </svg>
  );
}

function ClockIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="10" />
      <polyline points="12 6 12 12 16 14" />
    </svg>
  );
}

function UserIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
      <circle cx="12" cy="7" r="4" />
    </svg>
  );
}

/* ============================== 工具函数 ============================== */

/** 通过分页拉取所有条目并累加 size，计算存储用量；达到安全上限时标记 truncated */
async function computeUsage(): Promise<{ total: number; truncated: boolean }> {
  let total = 0;
  let before: number | undefined;
  let pages = 0;
  // 安全上限：100 页 × 100 条 = 10000 条
  while (pages < 100) {
    const res = await api.get<{ items: ClipItem[]; cursor: number }>('/api/clip', {
      limit: 100,
      ...(before ? { before } : {}),
    });
    const list = res.items ?? [];
    for (const it of list) total += it.size || 0;
    if (list.length < 100) return { total, truncated: false };
    before = res.cursor;
    pages++;
  }
  // 达到截断上限：实际用量可能更大，由调用方以 "≥" 展示
  return { total, truncated: true };
}

function getErrorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

/** 根据使用百分比返回进度条渐变样式（品牌色 → 警告/危险色） */
function usageBarStyle(percent: number): React.CSSProperties {
  if (percent >= 90) {
    return { background: 'linear-gradient(90deg, var(--warning), var(--danger))', width: `${percent}%` };
  }
  if (percent >= 70) {
    return { background: 'linear-gradient(90deg, var(--brand-400), var(--warning))', width: `${percent}%` };
  }
  return { background: 'linear-gradient(90deg, var(--brand-500), var(--brand-400))', width: `${percent}%` };
}

/* ============================== 页面组件 ============================== */

/**
 * 个人设置
 * - 修改密码：旧密码 + 新密码 + 确认新密码（≥8 且两次一致）
 * - 存储配置：保留天数
 * - 存储用量：已用 / 总配额 渐变进度条
 */
export default function Settings() {
  const { user, loading, logout } = useAuth();
  const navigate = useNavigate();
  const toast = useToast();

  const [profile, setProfile] = useState<User | null>(null);
  const [usage, setUsage] = useState(0);
  const [usageTruncated, setUsageTruncated] = useState(false);
  const [usageLoading, setUsageLoading] = useState(true);

  // 密码
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');

  // 保留天数
  const [retentionDays, setRetentionDays] = useState(30);

  const [saving, setSaving] = useState(false);
  const [loadingProfile, setLoadingProfile] = useState(true);

  const fetchProfile = useCallback(async () => {
    setLoadingProfile(true);
    try {
      const me = await api.get<User>('/api/me');
      setProfile(me);
      setRetentionDays(me.retention_days ?? 30);
    } catch (err) {
      toast.error(getErrorMessage(err, '加载个人信息失败'));
    } finally {
      setLoadingProfile(false);
    }
  }, [toast]);

  const fetchUsage = useCallback(async () => {
    setUsageLoading(true);
    try {
      const { total, truncated } = await computeUsage();
      setUsage(total);
      setUsageTruncated(truncated);
    } catch {
      // 用量计算失败不阻塞页面
      setUsage(0);
      setUsageTruncated(false);
    } finally {
      setUsageLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!user) return;
    fetchProfile();
    fetchUsage();
  }, [user, fetchProfile, fetchUsage]);

  const handleSavePassword = async () => {
    const changingPassword = !!newPassword || !!oldPassword || !!confirmPassword;
    if (!changingPassword) return;

    if (!oldPassword) {
      toast.error('请输入旧密码');
      return;
    }
    if (newPassword.length < 8) {
      toast.error('新密码长度至少 8 字符');
      return;
    }
    if (newPassword !== confirmPassword) {
      toast.error('两次输入的新密码不一致');
      return;
    }

    setSaving(true);
    try {
      await api.patch<User>('/api/me', {
        password: newPassword,
        old_password: oldPassword,
      });
      setOldPassword('');
      setNewPassword('');
      setConfirmPassword('');
      // 修改密码会使所有 refresh token 失效，需重新登录
      toast.success('密码已修改，请重新登录');
      await logout();
      navigate('/login', { replace: true });
    } catch (err) {
      toast.error(getErrorMessage(err, '保存失败'));
    } finally {
      setSaving(false);
    }
  };

  const handleSaveRetention = async () => {
    if (!retentionDays || retentionDays < 1 || retentionDays > 3650) {
      toast.error('保留天数需在 1-3650 之间');
      return;
    }
    if (retentionDays === profile?.retention_days) {
      toast.info('没有需要保存的更改');
      return;
    }

    setSaving(true);
    try {
      const updated = await api.patch<User>('/api/me', { retention_days: retentionDays });
      setProfile(updated);
      setRetentionDays(updated.retention_days ?? retentionDays);
      toast.success('设置已保存');
    } catch (err) {
      toast.error(getErrorMessage(err, '保存失败'));
    } finally {
      setSaving(false);
    }
  };

  if (loading || loadingProfile) {
    return (
      <div className="mx-auto w-full max-w-4xl space-y-8">
        <div>
          <Skeleton className="h-8 w-40" />
          <Skeleton className="mt-2 h-4 w-56" />
        </div>
        {[0, 1, 2].map((i) => (
          <Card key={i}>
            <CardHeader>
              <Skeleton className="h-5 w-32" />
            </CardHeader>
            <CardBody className="space-y-4">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </CardBody>
          </Card>
        ))}
      </div>
    );
  }
  if (!user || !profile) {
    return <Navigate to="/login" replace />;
  }

  const quota = profile.quota_bytes || 0;
  const percent = quota > 0 ? Math.min(100, (usage / quota) * 100) : 0;

  return (
    <div className="mx-auto w-full max-w-4xl space-y-8 animate-fade-in">
      {/* 页面标题 */}
      <div className="flex items-center gap-3">
        <div
          className="flex h-11 w-11 items-center justify-center rounded-xl text-white shadow-sm"
          style={{ background: 'linear-gradient(135deg, var(--brand-500), var(--brand-700))' }}
        >
          <UserIcon />
        </div>
        <div>
          <h1 className="text-xl font-bold text-[var(--text-primary)]">个人设置</h1>
          <p className="mt-0.5 text-sm text-[var(--text-muted)]">
            管理你的账号安全与存储配置 · {sanitizeText(profile.username)}
          </p>
        </div>
      </div>

      {/* 存储用量 */}
      <Card className="animate-slide-up">
        <CardHeader>
          <div className="flex items-center gap-2">
            <span className="text-[var(--brand-400)]"><DatabaseIcon /></span>
            <CardTitle>存储用量</CardTitle>
          </div>
        </CardHeader>
        <CardBody className="space-y-4">
          {usageLoading ? (
            <div className="space-y-3">
              <Skeleton className="h-4 w-48" />
              <Skeleton className="h-2.5 w-full rounded-full" />
              <Skeleton className="h-3 w-72" />
            </div>
          ) : (
            <>
              <div className="flex items-end justify-between">
                <div className="flex items-baseline gap-2">
                  <span className="text-2xl font-bold text-[var(--text-primary)]">
                    {usageTruncated ? `≥ ${formatBytes(usage)}` : formatBytes(usage)}
                  </span>
                  <span className="text-sm text-[var(--text-muted)]">/ {formatBytes(quota)}</span>
                </div>
                <span
                  className="text-sm font-semibold"
                  style={{ color: percent >= 90 ? 'var(--danger)' : percent >= 70 ? 'var(--warning)' : 'var(--brand-400)' }}
                >
                  {percent.toFixed(1)}%
                </span>
              </div>
              <div className="h-2.5 w-full overflow-hidden rounded-full bg-[var(--bg-hover)]">
                <div
                  className="h-full rounded-full transition-all duration-500"
                  style={usageBarStyle(percent)}
                />
              </div>
              <p className="text-xs text-[var(--text-muted)]">
                单条上限 {formatBytes(profile.max_item_size || 0)}，超出配额将无法上传新内容。
              </p>
            </>
          )}
        </CardBody>
      </Card>

      {/* 存储配置 */}
      <Card className="animate-slide-up">
        <CardHeader>
          <div className="flex items-center gap-2">
            <span className="text-[var(--brand-400)]"><ClockIcon /></span>
            <CardTitle>存储配置</CardTitle>
          </div>
        </CardHeader>
        <CardBody className="space-y-4">
          <Input
            id="retention"
            name="retention"
            type="number"
            min={1}
            max={3650}
            label="历史保留天数"
            value={retentionDays}
            onChange={(e) => setRetentionDays(Number(e.target.value))}
            disabled={saving}
            hint="超过保留天数的条目将被自动清理（取值范围 1-3650）。"
          />
          <div className="flex justify-end">
            <Button variant="primary" onClick={handleSaveRetention} loading={saving} disabled={saving}>
              保存配置
            </Button>
          </div>
        </CardBody>
      </Card>

      {/* 修改密码 */}
      <Card className="animate-slide-up">
        <CardHeader>
          <div className="flex items-center gap-2">
            <span className="text-[var(--brand-400)]"><KeyIcon /></span>
            <CardTitle>修改密码</CardTitle>
          </div>
        </CardHeader>
        <CardBody className="space-y-4">
          <Input
            id="old-pwd"
            name="old-pwd"
            type="password"
            label="旧密码"
            value={oldPassword}
            onChange={(e) => setOldPassword(e.target.value)}
            placeholder="请输入当前密码"
            autoComplete="current-password"
            disabled={saving}
          />
          <Input
            id="new-pwd"
            name="new-pwd"
            type="password"
            label="新密码"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            placeholder="至少 8 个字符"
            autoComplete="new-password"
            disabled={saving}
            hint="密码长度至少 8 个字符。"
          />
          <Input
            id="confirm-pwd"
            name="confirm-pwd"
            type="password"
            label="确认新密码"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            placeholder="再次输入新密码"
            autoComplete="new-password"
            disabled={saving}
          />
          <div
            className="flex items-start gap-2 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-3 py-2.5 text-xs text-[var(--text-secondary)]"
          >
            <svg className="mt-0.5 shrink-0 text-[var(--warning)]" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" /></svg>
            <span>修改密码后所有会话将被吊销，需要重新登录。</span>
          </div>
          <div className="flex justify-end">
            <Button variant="primary" onClick={handleSavePassword} loading={saving} disabled={saving}>
              修改密码
            </Button>
          </div>
        </CardBody>
      </Card>
    </div>
  );
}
