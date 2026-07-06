import { useCallback, useEffect, useState } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from '@/lib/auth';
import { api } from '@/lib/api';
import { formatTime, timeAgo, copyToClipboard, sanitizeText } from '@/lib/security';
import {
  Button,
  Input,
  Select,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  Skeleton,
  Badge,
  Modal,
  EmptyState,
  ConfirmDialog,
  useToast,
} from '@/components/ui';
import type { Device } from '@/types';

/* ============================== 常量 ============================== */

/** 设备类型选项（值与后端校验一致，android 对应移动端） */
const DEVICE_TYPES: { value: string; label: string }[] = [
  { value: 'web', label: 'Web 浏览器' },
  { value: 'ios-shortcut', label: 'iOS 快捷指令' },
  { value: 'desktop', label: '桌面端' },
  { value: 'android', label: '移动端 (Mobile)' },
];

const TYPE_VARIANT: Record<string, 'info' | 'success' | 'warning' | 'default'> = {
  web: 'info',
  'ios-shortcut': 'warning',
  desktop: 'success',
  android: 'default',
};

/* ============================== 图标 ============================== */

function SmartphoneIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <rect x="5" y="2" width="14" height="20" rx="2" ry="2" />
      <line x1="12" y1="18" x2="12.01" y2="18" />
    </svg>
  );
}

function PlusIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
      <line x1="12" y1="5" x2="12" y2="19" />
      <line x1="5" y1="12" x2="19" y2="12" />
    </svg>
  );
}

function CopyIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  );
}

function TrashIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <polyline points="3 6 5 6 21 6" />
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
    </svg>
  );
}

function ShieldOffIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M19.69 14A6.9 6.9 0 0 0 19 9m-3.91 6.91A6.9 6.9 0 0 1 12 16a6.9 6.9 0 0 1-5-2.18" />
      <path d="M12 2L4 5v6c0 5 3.4 9.5 8 11 4.6-1.5 8-6 8-11V5l-4-1.5" />
      <line x1="3" y1="3" x2="21" y2="21" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
      <polyline points="20 6 9 17 4 12" />
    </svg>
  );
}

/* ============================== 辅助函数 ============================== */

function typeLabel(t: string): string {
  return DEVICE_TYPES.find((x) => x.value === t)?.label ?? t;
}

/**
 * 判断设备是否已颁发 Token。
 * Device 类型定义了 api_token_hash，后端实际返回 has_token（布尔），
 * 这里两者兼容检查，确保类型安全且运行时正确。
 */
function deviceHasToken(d: Device): boolean {
  return Boolean(d.api_token_hash) || Boolean((d as Device & { has_token?: unknown }).has_token);
}

function getErrorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

/* ============================== 页面组件 ============================== */

/**
 * 设备管理
 * - 设备列表：名称、类型、创建时间、最后活跃、操作（响应式：桌面表格 / 移动端卡片）
 * - 创建设备弹窗（创建后一次性展示 API Token）
 * - 吊销 Token（确认弹窗）
 * - 删除设备（确认弹窗）
 */
export default function Devices() {
  const { user, loading } = useAuth();
  const toast = useToast();

  const [devices, setDevices] = useState<Device[]>([]);
  const [loadingList, setLoadingList] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);

  // 创建弹窗
  const [createOpen, setCreateOpen] = useState(false);
  const [newName, setNewName] = useState('');
  const [newType, setNewType] = useState('web');

  // 创建后 Token 展示
  const [createdToken, setCreatedToken] = useState<{ name: string; token: string } | null>(null);
  const [copied, setCopied] = useState(false);

  // 确认弹窗
  const [revokeTarget, setRevokeTarget] = useState<Device | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Device | null>(null);

  const fetchDevices = useCallback(async () => {
    setLoadingList(true);
    try {
      const res = await api.get<{ devices: Device[] }>('/api/devices');
      setDevices(res.devices ?? []);
    } catch (err) {
      toast.error(getErrorMessage(err, '加载设备列表失败'));
    } finally {
      setLoadingList(false);
    }
  }, [toast]);

  useEffect(() => {
    if (!user) return;
    fetchDevices();
  }, [user, fetchDevices]);

  const resetCreateForm = () => {
    setNewName('');
    setNewType('web');
  };

  const handleCreate = async () => {
    const name = newName.trim();
    if (!name) {
      toast.error('请填写设备名称');
      return;
    }
    if (name.length > 64) {
      toast.error('设备名称不能超过 64 字符');
      return;
    }
    setActionLoading(true);
    try {
      const created = await api.post<Device>('/api/devices', { name, type: newType });
      // 后端仅返回一次明文 token
      if (created.api_token) {
        setCreatedToken({ name: created.name, token: created.api_token });
        setCopied(false);
      }
      setCreateOpen(false);
      resetCreateForm();
      toast.success('设备创建成功');
      fetchDevices();
    } catch (err) {
      toast.error(getErrorMessage(err, '创建设备失败'));
    } finally {
      setActionLoading(false);
    }
  };

  const handleRevoke = async () => {
    if (!revokeTarget || actionLoading) return;
    setActionLoading(true);
    try {
      await api.post(`/api/devices/${revokeTarget.id}/revoke`);
      toast.success('Token 已吊销');
      setRevokeTarget(null);
      fetchDevices();
    } catch (err) {
      toast.error(getErrorMessage(err, '吊销失败'));
    } finally {
      setActionLoading(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget || actionLoading) return;
    setActionLoading(true);
    try {
      await api.del(`/api/devices/${deleteTarget.id}`);
      toast.success('设备已删除');
      setDeleteTarget(null);
      fetchDevices();
    } catch (err) {
      toast.error(getErrorMessage(err, '删除失败'));
    } finally {
      setActionLoading(false);
    }
  };

  const handleCopyToken = async () => {
    if (!createdToken) return;
    const ok = await copyToClipboard(createdToken.token);
    if (ok) {
      setCopied(true);
      toast.success('Token 已复制');
    } else {
      toast.error('复制失败，请手动复制');
    }
  };

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Skeleton className="h-5 w-5 rounded-full" />
      </div>
    );
  }
  if (!user) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="space-y-8 animate-fade-in">
      {/* 页面标题 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div
            className="flex h-11 w-11 items-center justify-center rounded-xl text-white shadow-sm"
            style={{ background: 'linear-gradient(135deg, var(--brand-500), var(--brand-700))' }}
          >
            <SmartphoneIcon />
          </div>
          <div>
            <h1 className="text-xl font-bold text-[var(--text-primary)]">设备管理</h1>
            <p className="mt-0.5 text-sm text-[var(--text-muted)]">
              管理可访问账号的设备与其 API Token
            </p>
          </div>
        </div>
        <Button variant="primary" onClick={() => setCreateOpen(true)}>
          <PlusIcon />
          创建设备
        </Button>
      </div>

      {/* 设备列表 */}
      <Card className="animate-slide-up">
        <CardHeader>
          <CardTitle>已绑定设备</CardTitle>
          <span className="text-xs text-[var(--text-muted)]">共 {devices.length} 台</span>
        </CardHeader>
        <CardBody className="p-0">
          {loadingList ? (
            <div className="space-y-3 p-5">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-14 w-full" />
              ))}
            </div>
          ) : devices.length === 0 ? (
            <EmptyState
              icon={<SmartphoneIcon />}
              title="还没有设备"
              description="创建设备后可获得 API Token，用于 iPhone 快捷指令、桌面端等场景接入。"
              action={
                <Button variant="primary" onClick={() => setCreateOpen(true)}>
                  <PlusIcon />
                  创建第一个设备
                </Button>
              }
            />
          ) : (
            <>
              {/* 桌面表格 */}
              <div className="hidden overflow-x-auto md:block">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-[var(--border-subtle)] text-left text-xs text-[var(--text-muted)]">
                      <th className="px-5 py-3 font-medium">设备名称</th>
                      <th className="px-5 py-3 font-medium">类型</th>
                      <th className="px-5 py-3 font-medium">Token</th>
                      <th className="px-5 py-3 font-medium">创建时间</th>
                      <th className="px-5 py-3 font-medium">最后活跃</th>
                      <th className="px-5 py-3 text-right font-medium">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {devices.map((d) => (
                      <tr
                        key={d.id}
                        className="border-b border-[var(--border-subtle)] transition-colors last:border-0 hover:bg-[var(--bg-hover)]"
                      >
                        <td className="px-5 py-3.5 font-medium text-[var(--text-primary)]">
                          {sanitizeText(d.name)}
                        </td>
                        <td className="px-5 py-3.5">
                          <Badge variant={TYPE_VARIANT[d.type] ?? 'default'}>{typeLabel(d.type)}</Badge>
                        </td>
                        <td className="px-5 py-3.5">
                          {deviceHasToken(d) ? (
                            <span className="inline-flex items-center gap-1 text-[var(--success)]">
                              <CheckIcon /> 已颁发
                            </span>
                          ) : (
                            <span className="text-[var(--text-muted)]">未颁发</span>
                          )}
                        </td>
                        <td className="px-5 py-3.5 text-[var(--text-secondary)]" title={formatTime(d.created_at)}>
                          {timeAgo(d.created_at)}
                        </td>
                        <td className="px-5 py-3.5 text-[var(--text-secondary)]">
                          {d.last_seen_at ? (
                            <span title={formatTime(d.last_seen_at)}>{timeAgo(d.last_seen_at)}</span>
                          ) : (
                            <span className="text-[var(--text-muted)]">—</span>
                          )}
                        </td>
                        <td className="px-5 py-3.5">
                          <div className="flex justify-end gap-2">
                            {deviceHasToken(d) && (
                              <Button size="sm" variant="outline" onClick={() => setRevokeTarget(d)}>
                                <ShieldOffIcon />
                                吊销
                              </Button>
                            )}
                            <Button size="sm" variant="danger" onClick={() => setDeleteTarget(d)}>
                              <TrashIcon />
                              删除
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {/* 移动端卡片 */}
              <div className="space-y-3 p-4 md:hidden">
                {devices.map((d) => (
                  <div
                    key={d.id}
                    className="rounded-xl border border-[var(--border-default)] bg-[var(--bg-input)] p-4"
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <p className="truncate font-medium text-[var(--text-primary)]">{sanitizeText(d.name)}</p>
                        <div className="mt-1.5 flex flex-wrap items-center gap-2">
                          <Badge variant={TYPE_VARIANT[d.type] ?? 'default'}>{typeLabel(d.type)}</Badge>
                          {deviceHasToken(d) ? (
                            <span className="inline-flex items-center gap-1 text-xs text-[var(--success)]">
                              <CheckIcon /> 已颁发
                            </span>
                          ) : (
                            <span className="text-xs text-[var(--text-muted)]">未颁发</span>
                          )}
                        </div>
                      </div>
                    </div>
                    <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-[var(--text-muted)]">
                      <div>
                        <span className="block">创建时间</span>
                        <span className="text-[var(--text-secondary)]" title={formatTime(d.created_at)}>
                          {timeAgo(d.created_at)}
                        </span>
                      </div>
                      <div>
                        <span className="block">最后活跃</span>
                        {d.last_seen_at ? (
                          <span className="text-[var(--text-secondary)]" title={formatTime(d.last_seen_at)}>
                            {timeAgo(d.last_seen_at)}
                          </span>
                        ) : (
                          <span>—</span>
                        )}
                      </div>
                    </div>
                    <div className="mt-3 flex gap-2">
                      {deviceHasToken(d) && (
                        <Button size="sm" variant="outline" className="flex-1" onClick={() => setRevokeTarget(d)}>
                          <ShieldOffIcon />
                          吊销
                        </Button>
                      )}
                      <Button size="sm" variant="danger" className="flex-1" onClick={() => setDeleteTarget(d)}>
                        <TrashIcon />
                        删除
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}
        </CardBody>
      </Card>

      {/* 创建设备弹窗 */}
      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title="创建设备">
        <div className="space-y-4">
          <Input
            label="设备名称"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="例如：iPhone 快捷指令"
            autoFocus
            disabled={actionLoading}
            maxLength={64}
            hint="用于识别设备，最多 64 个字符。"
          />
          <Select
            label="设备类型"
            value={newType}
            onChange={(e) => setNewType(e.target.value)}
            options={DEVICE_TYPES}
            disabled={actionLoading}
          />
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="ghost" onClick={() => setCreateOpen(false)} disabled={actionLoading}>
              取消
            </Button>
            <Button variant="primary" onClick={handleCreate} loading={actionLoading} disabled={actionLoading}>
              创建
            </Button>
          </div>
        </div>
      </Modal>

      {/* 创建后 Token 一次性展示 */}
      <Modal
        open={!!createdToken}
        onClose={() => setCreatedToken(null)}
        title="API Token（仅显示一次）"
        closeOnBackdrop={false}
      >
        {createdToken && (
          <div className="space-y-4">
            <div className="flex items-start gap-2 rounded-lg border border-[var(--warning)]/40 bg-[var(--warning)]/10 px-3 py-2.5 text-sm text-[var(--warning)]">
              <svg className="mt-0.5 shrink-0" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" /></svg>
              <span>设备「{sanitizeText(createdToken.name)}」的 API Token 已生成。请立即复制保存，关闭后将无法再次查看。</span>
            </div>
            <div className="break-all rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-3.5 py-3 font-mono text-sm text-[var(--success)]">
              {createdToken.token}
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="secondary" onClick={handleCopyToken}>
                {copied ? <CheckIcon /> : <CopyIcon />}
                {copied ? '已复制' : '复制 Token'}
              </Button>
              <Button variant="primary" onClick={() => setCreatedToken(null)}>
                我已保存
              </Button>
            </div>
          </div>
        )}
      </Modal>

      {/* 吊销 Token 确认 */}
      <ConfirmDialog
        open={!!revokeTarget}
        title="吊销 API Token"
        message={`确定要吊销设备「${revokeTarget ? sanitizeText(revokeTarget.name) : ''}」的 API Token 吗？吊销后该设备将无法再通过 API Token 访问。`}
        confirmText="确认吊销"
        variant="danger"
        loading={actionLoading}
        onConfirm={handleRevoke}
        onCancel={() => setRevokeTarget(null)}
      />

      {/* 删除设备确认 */}
      <ConfirmDialog
        open={!!deleteTarget}
        title="删除设备"
        message={`确定要删除设备「${deleteTarget ? sanitizeText(deleteTarget.name) : ''}」吗？此操作不可恢复。`}
        confirmText="确认删除"
        variant="danger"
        loading={actionLoading}
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  );
}
