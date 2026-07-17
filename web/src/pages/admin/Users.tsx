import { useEffect, useState, useCallback } from 'react';
import { api } from '@/lib/api';
import { useAuth } from '@/lib/auth';
import { sanitizeText, formatBytes, timeAgo } from '@/lib/security';
import {
  Button,
  Input,
  Select,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  Modal,
  Skeleton,
  Badge,
  useToast,
  EmptyState,
  ConfirmDialog,
} from '@/components/ui';
import type { User } from '@/types';

/* ============================== 常量 ============================== */

const PAGE_SIZE = 20;
const MB = 1024 * 1024;
const GB = 1024 * 1024 * 1024;

const ROLE_OPTIONS = [
  { value: 'user', label: '普通用户' },
  { value: 'admin', label: '管理员' },
];

const STATUS_OPTIONS = [
  { value: 'active', label: '正常' },
  { value: 'disabled', label: '已禁用' },
];

/* ============================== 图标 ============================== */

function UsersIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
      <path d="M16 3.13a4 4 0 0 1 0 7.75" />
    </svg>
  );
}

function EditIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
      <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
    </svg>
  );
}

function KeyIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />
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

function ChevronLeftIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <polyline points="15 18 9 12 15 6" />
    </svg>
  );
}

function ChevronRightIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <polyline points="9 18 15 12 9 6" />
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

/* ============================== 辅助函数 ============================== */

function getErrorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

function bytesToMB(bytes: number): string {
  return (bytes / MB).toString();
}

function bytesToGB(bytes: number): string {
  return (bytes / GB).toString();
}

/* ============================== 编辑表单类型 ============================== */

interface EditFormData {
  role: 'user' | 'admin';
  status: 'active' | 'disabled';
  maxItemSizeMB: string;
  quotaGB: string;
  retentionDays: string;
}

/* ============================== 列表响应类型 ============================== */

interface UserListResponse {
  items?: User[];
  users?: User[];
  total?: number;
  limit?: number;
  offset?: number;
}

/* ============================== 主组件 ============================== */

/**
 * 用户管理
 * - 用户列表表格（响应式：桌面表格 / 移动端卡片）
 * - 编辑用户（Modal）：角色、状态、单条上限、总配额、保留天数
 * - 重置密码（Modal）：新密码 + 确认密码（≥8 且一致）
 * - 启用/禁用（ConfirmDialog）：当前管理员不可禁用自己
 */
export default function Users() {
  const { user: currentUser } = useAuth();
  const toast = useToast();

  // 列表状态
  const [users, setUsers] = useState<User[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(0);
  const [hasMore, setHasMore] = useState(false);

  // 编辑弹窗状态
  const [editingUser, setEditingUser] = useState<User | null>(null);
  const [editForm, setEditForm] = useState<EditFormData>({
    role: 'user',
    status: 'active',
    maxItemSizeMB: '10',
    quotaGB: '1',
    retentionDays: '30',
  });
  const [editLoading, setEditLoading] = useState(false);

  // 重置密码弹窗状态
  const [resetUser, setResetUser] = useState<User | null>(null);
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [resetLoading, setResetLoading] = useState(false);

  // 启用/禁用确认弹窗状态
  const [toggleUser, setToggleUser] = useState<User | null>(null);
  const [toggleLoading, setToggleLoading] = useState(false);

  /* ---------- 获取用户列表 ---------- */
  const fetchUsers = useCallback(async () => {
    try {
      setError(null);
      const offset = page * PAGE_SIZE;
      const data = await api.get<UserListResponse>('/api/admin/users', {
        limit: PAGE_SIZE,
        offset,
      });
      // 兼容 {items} / {users} / 数组 三种返回格式
      const list = data.items ?? data.users ?? (Array.isArray(data) ? (data as User[]) : []);
      // 空页（如末页满员后翻页、或末页数据被删空）时自动回退一页，避免分页死路
      if (list.length === 0 && page > 0) {
        setPage((p) => Math.max(0, p - 1));
        return;
      }
      const totalCount = typeof data.total === 'number' ? data.total : list.length;
      setUsers(list);
      setTotal(totalCount);
      // 有 total 时按总数判断是否还有下一页；无 total 时退化为“满页即还有”
      setHasMore(
        typeof data.total === 'number'
          ? offset + list.length < totalCount
          : list.length === PAGE_SIZE,
      );
    } catch (err) {
      setError(getErrorMessage(err, '用户列表加载失败'));
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => {
    setLoading(true);
    fetchUsers();
  }, [fetchUsers]);

  /* ---------- 编辑用户 ---------- */
  const openEditModal = (u: User) => {
    setEditingUser(u);
    setEditForm({
      role: u.role,
      status: u.status,
      maxItemSizeMB: bytesToMB(u.max_item_size),
      quotaGB: bytesToGB(u.quota_bytes),
      retentionDays: String(u.retention_days),
    });
  };

  const closeEditModal = () => {
    setEditingUser(null);
  };

  const handleEditSubmit = async () => {
    if (!editingUser) return;

    const maxSizeMB = Number(editForm.maxItemSizeMB);
    const quotaGB = Number(editForm.quotaGB);
    const retention = Number(editForm.retentionDays);

    if (!maxSizeMB || maxSizeMB <= 0) {
      toast.error('单条上限必须大于 0');
      return;
    }
    if (!quotaGB || quotaGB <= 0) {
      toast.error('总配额必须大于 0');
      return;
    }
    if (!retention || retention < 1 || retention > 3650) {
      toast.error('保留天数需在 1-3650 之间');
      return;
    }

    try {
      setEditLoading(true);
      await api.patch(`/api/admin/users/${editingUser.id}`, {
        role: editForm.role,
        status: editForm.status,
        max_item_size: Math.round(maxSizeMB * MB),
        quota_bytes: Math.round(quotaGB * GB),
        retention_days: Math.round(retention),
      });
      toast.success(`用户 ${editingUser.username} 已更新`);
      closeEditModal();
      fetchUsers();
    } catch (err) {
      toast.error(getErrorMessage(err, '更新用户失败'));
    } finally {
      setEditLoading(false);
    }
  };

  /* ---------- 重置密码 ---------- */
  const openResetModal = (u: User) => {
    setResetUser(u);
    setNewPassword('');
    setConfirmPassword('');
  };

  const closeResetModal = () => {
    setResetUser(null);
    setNewPassword('');
    setConfirmPassword('');
  };

  const handleResetSubmit = async () => {
    if (!resetUser) return;

    if (newPassword.length < 8) {
      toast.error('密码长度至少 8 位');
      return;
    }
    if (newPassword !== confirmPassword) {
      toast.error('两次输入的密码不一致');
      return;
    }

    try {
      setResetLoading(true);
      await api.post(`/api/admin/users/${resetUser.id}/reset-password`, {
        password: newPassword,
      });
      toast.success(`用户 ${resetUser.username} 的密码已重置`);
      closeResetModal();
    } catch (err) {
      toast.error(getErrorMessage(err, '重置密码失败'));
    } finally {
      setResetLoading(false);
    }
  };

  /* ---------- 启用/禁用用户 ---------- */
  const handleToggleStatus = async () => {
    if (!toggleUser) return;

    const newStatus = toggleUser.status === 'active' ? 'disabled' : 'active';
    try {
      setToggleLoading(true);
      await api.patch(`/api/admin/users/${toggleUser.id}`, {
        status: newStatus,
      });
      toast.success(`用户 ${toggleUser.username} 已${newStatus === 'active' ? '启用' : '禁用'}`);
      setToggleUser(null);
      fetchUsers();
    } catch (err) {
      toast.error(getErrorMessage(err, '操作失败'));
    } finally {
      setToggleLoading(false);
    }
  };

  /* ---------- 渲染 ---------- */

  if (loading && users.length === 0 && !error) {
    return (
      <div className="space-y-6">
        <div>
          <Skeleton className="h-7 w-40" />
          <Skeleton className="mt-2 h-4 w-56" />
        </div>
        <Card>
          <CardBody className="space-y-3 p-5">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-14 w-full" />
            ))}
          </CardBody>
        </Card>
      </div>
    );
  }

  if (error && users.length === 0) {
    return (
      <div className="space-y-8 animate-fade-in">
        <div>
          <h1 className="text-xl font-bold text-[var(--text-primary)]">用户管理</h1>
          <p className="mt-1 text-sm text-[var(--text-muted)]">管理平台用户、配额与权限</p>
        </div>
        <Card>
          <CardBody className="flex flex-col items-center justify-center gap-4 py-20">
            <div className="text-[var(--warning)]">
              <AlertTriangleIcon />
            </div>
            <div className="text-center">
              <p className="text-lg font-medium text-[var(--text-primary)]">用户列表加载失败</p>
              <p className="mt-1 text-sm text-[var(--text-secondary)]">{error}</p>
              <p className="mt-1 text-xs text-[var(--text-muted)]">
                管理后台用户接口可能尚未启用，请确认后端已实现 /api/admin/users
              </p>
            </div>
            <Button
              variant="primary"
              onClick={() => {
                setLoading(true);
                fetchUsers();
              }}
            >
              重试
            </Button>
          </CardBody>
        </Card>
      </div>
    );
  }

  const startIdx = page * PAGE_SIZE + 1;
  const endIdx = page * PAGE_SIZE + users.length;
  // 编辑对象为当前登录管理员自己时，禁止修改角色/状态，防止唯一管理员自我降权
  const editingIsSelf = editingUser !== null && currentUser?.id === editingUser.id;

  return (
    <div className="space-y-8 animate-fade-in">
      {/* 标题栏 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-bold text-[var(--text-primary)]">用户管理</h1>
          <p className="mt-1 text-sm text-[var(--text-muted)]">管理平台用户、配额与权限</p>
        </div>
        <Button variant="outline" size="sm" onClick={fetchUsers} loading={loading}>
          {!loading && <RefreshIcon />}
          刷新
        </Button>
      </div>

      {/* 错误提示（有数据但刷新失败） */}
      {error && users.length > 0 && (
        <div className="flex items-start gap-2 rounded-lg border border-[var(--warning)]/40 bg-[var(--warning)]/10 px-4 py-3 text-sm text-[var(--warning)]">
          <AlertTriangleIcon size={16} />
          <span>刷新失败：{error}，当前显示的是上次缓存的数据。</span>
        </div>
      )}

      {/* 用户列表 */}
      {users.length === 0 ? (
        <Card className="animate-slide-up">
          <CardBody className="py-16">
            <EmptyState
              icon={<UsersIcon />}
              title="暂无用户"
              description="系统中还没有注册用户。"
            />
          </CardBody>
        </Card>
      ) : (
        <Card className="animate-slide-up">
          <CardHeader>
            <CardTitle>用户列表</CardTitle>
            <span className="text-xs text-[var(--text-muted)]">
              {startIdx}-{endIdx} / 共 {total} 条
            </span>
          </CardHeader>
          <CardBody className="p-0">
            {/* 桌面表格 */}
            <div className="hidden overflow-x-auto md:block">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-[var(--border-subtle)] text-left text-xs text-[var(--text-muted)]">
                    <th className="px-5 py-3 font-medium">用户名</th>
                    <th className="px-5 py-3 font-medium">角色</th>
                    <th className="px-5 py-3 font-medium">状态</th>
                    <th className="px-5 py-3 font-medium">总配额</th>
                    <th className="px-5 py-3 font-medium">保留天数</th>
                    <th className="px-5 py-3 font-medium">创建时间</th>
                    <th className="px-5 py-3 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {users.map((u) => {
                    const isSelf = currentUser?.id === u.id;
                    return (
                      <tr
                        key={u.id}
                        className="border-b border-[var(--border-subtle)] transition-colors last:border-0 hover:bg-[var(--bg-hover)]"
                      >
                        {/* 用户名 */}
                        <td className="px-5 py-3.5">
                          <div className="flex items-center gap-2.5">
                            <div
                              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold text-white"
                              style={{ background: 'linear-gradient(135deg, var(--brand-400), var(--brand-600))' }}
                            >
                              {sanitizeText(u.username).slice(0, 2).toUpperCase()}
                            </div>
                            <div className="min-w-0">
                              <p className="font-medium text-[var(--text-primary)]">
                                {sanitizeText(u.username)}
                                {isSelf && (
                                  <span className="ml-1.5 text-xs text-[var(--text-muted)]">(你)</span>
                                )}
                              </p>
                              {u.email && (
                                <p className="truncate text-xs text-[var(--text-muted)]">
                                  {sanitizeText(u.email)}
                                </p>
                              )}
                            </div>
                          </div>
                        </td>

                        {/* 角色 */}
                        <td className="px-5 py-3.5">
                          <Badge variant={u.role === 'admin' ? 'info' : 'default'}>
                            {u.role === 'admin' ? '管理员' : '普通用户'}
                          </Badge>
                        </td>

                        {/* 状态 */}
                        <td className="px-5 py-3.5">
                          <Badge variant={u.status === 'active' ? 'success' : 'danger'}>
                            {u.status === 'active' ? '正常' : '已禁用'}
                          </Badge>
                        </td>

                        {/* 总配额 */}
                        <td className="px-5 py-3.5 text-[var(--text-secondary)]">
                          {formatBytes(u.quota_bytes)}
                        </td>

                        {/* 保留天数 */}
                        <td className="px-5 py-3.5 text-[var(--text-secondary)]">
                          {u.retention_days} 天
                        </td>

                        {/* 创建时间 */}
                        <td className="px-5 py-3.5 text-[var(--text-secondary)]">
                          <span title={u.created_at}>{timeAgo(u.created_at)}</span>
                        </td>

                        {/* 操作 */}
                        <td className="px-5 py-3.5">
                          <div className="flex items-center justify-end gap-2">
                            <Button size="sm" variant="outline" onClick={() => openEditModal(u)}>
                              <EditIcon />
                              编辑
                            </Button>
                            <Button size="sm" variant="ghost" onClick={() => openResetModal(u)}>
                              <KeyIcon />
                              重置密码
                            </Button>
                            {!isSelf && (
                              <Button
                                size="sm"
                                variant={u.status === 'active' ? 'danger' : 'secondary'}
                                onClick={() => setToggleUser(u)}
                              >
                                {u.status === 'active' ? '禁用' : '启用'}
                              </Button>
                            )}
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            {/* 移动端卡片 */}
            <div className="space-y-3 p-4 md:hidden">
              {users.map((u) => {
                const isSelf = currentUser?.id === u.id;
                return (
                  <div
                    key={u.id}
                    className="rounded-xl border border-[var(--border-default)] bg-[var(--bg-input)] p-4"
                  >
                    <div className="flex items-center gap-2.5">
                      <div
                        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-xs font-semibold text-white"
                        style={{ background: 'linear-gradient(135deg, var(--brand-400), var(--brand-600))' }}
                      >
                        {sanitizeText(u.username).slice(0, 2).toUpperCase()}
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="truncate font-medium text-[var(--text-primary)]">
                          {sanitizeText(u.username)}
                          {isSelf && <span className="ml-1 text-xs text-[var(--text-muted)]">(你)</span>}
                        </p>
                        {u.email && (
                          <p className="truncate text-xs text-[var(--text-muted)]">{sanitizeText(u.email)}</p>
                        )}
                      </div>
                    </div>
                    <div className="mt-3 flex flex-wrap items-center gap-2">
                      <Badge variant={u.role === 'admin' ? 'info' : 'default'}>
                        {u.role === 'admin' ? '管理员' : '普通用户'}
                      </Badge>
                      <Badge variant={u.status === 'active' ? 'success' : 'danger'}>
                        {u.status === 'active' ? '正常' : '已禁用'}
                      </Badge>
                    </div>
                    <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-[var(--text-muted)]">
                      <div>
                        <span className="block">总配额</span>
                        <span className="text-[var(--text-secondary)]">{formatBytes(u.quota_bytes)}</span>
                      </div>
                      <div>
                        <span className="block">保留天数</span>
                        <span className="text-[var(--text-secondary)]">{u.retention_days} 天</span>
                      </div>
                      <div>
                        <span className="block">创建时间</span>
                        <span className="text-[var(--text-secondary)]" title={u.created_at}>
                          {timeAgo(u.created_at)}
                        </span>
                      </div>
                    </div>
                    <div className="mt-3 flex flex-wrap gap-2">
                      <Button size="sm" variant="outline" className="flex-1" onClick={() => openEditModal(u)}>
                        <EditIcon />
                        编辑
                      </Button>
                      <Button size="sm" variant="ghost" className="flex-1" onClick={() => openResetModal(u)}>
                        <KeyIcon />
                        重置密码
                      </Button>
                      {!isSelf && (
                        <Button
                          size="sm"
                          variant={u.status === 'active' ? 'danger' : 'secondary'}
                          className="flex-1"
                          onClick={() => setToggleUser(u)}
                        >
                          {u.status === 'active' ? '禁用' : '启用'}
                        </Button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </CardBody>
        </Card>
      )}

      {/* 分页：只要总数非空就渲染，避免空页时控件整个消失无法返回 */}
      {total > 0 && (
        <div className="flex items-center justify-between">
          <p className="text-sm text-[var(--text-muted)]">
            第 {page + 1} 页 · 每页 {PAGE_SIZE} 条
          </p>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              disabled={page === 0 || loading}
            >
              <ChevronLeftIcon />
              上一页
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPage((p) => p + 1)}
              disabled={!hasMore || loading}
            >
              下一页
              <ChevronRightIcon />
            </Button>
          </div>
        </div>
      )}

      {/* 编辑用户弹窗 */}
      <Modal
        open={!!editingUser}
        onClose={closeEditModal}
        title={`编辑用户 — ${editingUser ? sanitizeText(editingUser.username) : ''}`}
        size="lg"
      >
        <div className="space-y-4">
          {editingIsSelf && (
            <div className="flex items-start gap-2 rounded-lg border border-[var(--warning)]/40 bg-[var(--warning)]/10 px-3 py-2.5 text-xs text-[var(--warning)]">
              <AlertTriangleIcon size={14} />
              <span>不能修改自己的角色与状态，防止管理员自我降权或禁用。</span>
            </div>
          )}
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Select
              label="角色"
              value={editForm.role}
              onChange={(e) => setEditForm((f) => ({ ...f, role: e.target.value as 'user' | 'admin' }))}
              options={ROLE_OPTIONS}
              disabled={editLoading || editingIsSelf}
            />
            <Select
              label="状态"
              value={editForm.status}
              onChange={(e) => setEditForm((f) => ({ ...f, status: e.target.value as 'active' | 'disabled' }))}
              options={STATUS_OPTIONS}
              disabled={editLoading || editingIsSelf}
            />
          </div>
          <Input
            label="单条上限 (MB)"
            type="number"
            value={editForm.maxItemSizeMB}
            onChange={(e) => setEditForm((f) => ({ ...f, maxItemSizeMB: e.target.value }))}
            placeholder="10"
            min={1}
            disabled={editLoading}
            hint={`当前：${formatBytes(editingUser?.max_item_size ?? 0)}`}
          />
          <Input
            label="总配额 (GB)"
            type="number"
            value={editForm.quotaGB}
            onChange={(e) => setEditForm((f) => ({ ...f, quotaGB: e.target.value }))}
            placeholder="1"
            min={0.1}
            step={0.1}
            disabled={editLoading}
            hint={`当前：${formatBytes(editingUser?.quota_bytes ?? 0)}`}
          />
          <Input
            label="保留天数"
            type="number"
            value={editForm.retentionDays}
            onChange={(e) => setEditForm((f) => ({ ...f, retentionDays: e.target.value }))}
            placeholder="30"
            min={1}
            max={3650}
            disabled={editLoading}
            hint="取值范围 1-3650 天。"
          />
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="ghost" onClick={closeEditModal} disabled={editLoading}>
              取消
            </Button>
            <Button variant="primary" onClick={handleEditSubmit} loading={editLoading}>
              保存
            </Button>
          </div>
        </div>
      </Modal>

      {/* 重置密码弹窗 */}
      <Modal
        open={!!resetUser}
        onClose={closeResetModal}
        title={`重置密码 — ${resetUser ? sanitizeText(resetUser.username) : ''}`}
        size="sm"
      >
        <div className="space-y-4">
          <div className="flex items-start gap-2 rounded-lg border border-[var(--warning)]/40 bg-[var(--warning)]/10 px-3 py-2.5 text-xs text-[var(--warning)]">
            <AlertTriangleIcon size={14} />
            <span>重置密码后，该用户的所有会话将被吊销，需要重新登录。</span>
          </div>
          <Input
            label="新密码"
            type="password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            placeholder="至少 8 位"
            autoComplete="new-password"
            disabled={resetLoading}
            hint="密码长度至少 8 位。"
          />
          <Input
            label="确认密码"
            type="password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            placeholder="再次输入新密码"
            autoComplete="new-password"
            disabled={resetLoading}
          />
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="ghost" onClick={closeResetModal} disabled={resetLoading}>
              取消
            </Button>
            <Button variant="danger" onClick={handleResetSubmit} loading={resetLoading}>
              确认重置
            </Button>
          </div>
        </div>
      </Modal>

      {/* 启用/禁用确认弹窗 */}
      <ConfirmDialog
        open={!!toggleUser}
        title={toggleUser?.status === 'active' ? '禁用用户' : '启用用户'}
        message={
          toggleUser
            ? `确定要${toggleUser.status === 'active' ? '禁用' : '启用'}用户「${sanitizeText(toggleUser.username)}」吗？${
                toggleUser.status === 'active'
                  ? '禁用后该用户将无法登录和使用服务。'
                  : '启用后该用户可以正常登录和使用服务。'
              }`
            : ''
        }
        confirmText={toggleUser?.status === 'active' ? '确认禁用' : '确认启用'}
        cancelText="取消"
        variant={toggleUser?.status === 'active' ? 'danger' : 'primary'}
        loading={toggleLoading}
        onConfirm={handleToggleStatus}
        onCancel={() => setToggleUser(null)}
      />
    </div>
  );
}
