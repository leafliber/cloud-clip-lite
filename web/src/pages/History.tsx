import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from '@/lib/auth';
import { wsClient } from '@/lib/ws';
import { api, tokenStorage } from '@/lib/api';
import {
  sanitizeText,
  formatBytes,
  formatTime,
  timeAgo,
  copyToClipboard,
  downloadBlob,
  cn,
} from '@/lib/security';
import {
  Button,
  Input,
  Spinner,
  EmptyState,
  Modal,
  Badge,
  Skeleton,
  useToast,
} from '@/components/ui';
import type { ClipItem, ClipListResponse } from '@/types';

const PAGE_SIZE = 20;

type TypeFilter = 'all' | 'text' | 'image' | 'file';

const TYPE_TABS: { key: TypeFilter; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'text', label: '文本' },
  { key: 'image', label: '图片' },
  { key: 'file', label: '文件' },
];

/** 取当前访问令牌（用于二进制内容鉴权） */
function authToken(): string {
  return tokenStorage.getAccessToken() ?? '';
}

function authHeaders(): HeadersInit {
  const token = authToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/** 下载条目内容：带鉴权拉取 blob 后触发下载 */
async function downloadClipContent(id: number, filename: string): Promise<void> {
  const res = await fetch(`/api/clip/${id}/content`, { headers: authHeaders() });
  if (!res.ok) throw new Error('下载失败');
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  downloadBlob(url, filename);
}

/** 类型 → Badge 变体 */
function typeVariant(type: ClipItem['type']): 'info' | 'success' | 'warning' {
  return type === 'text' ? 'info' : type === 'image' ? 'success' : 'warning';
}

/** 类型 → 中文标签 */
function typeLabel(type: ClipItem['type']): string {
  return type === 'text' ? '文本' : type === 'image' ? '图片' : '文件';
}

/** 图片缩略图：带鉴权拉取 blob 并生成 objectURL，卸载时回收 */
function ClipImage({
  id,
  onClick,
  className,
}: {
  id: number;
  onClick?: () => void;
  className?: string;
}) {
  const [url, setUrl] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let revoked = false;
    let objectUrl: string | null = null;
    setUrl(null);
    setFailed(false);

    fetch(`/api/clip/${id}/content`, { headers: authHeaders() })
      .then((res) => (res.ok ? res.blob() : Promise.reject(new Error('fetch failed'))))
      .then((blob) => {
        if (revoked) return;
        objectUrl = URL.createObjectURL(blob);
        setUrl(objectUrl);
      })
      .catch(() => {
        if (!revoked) setFailed(true);
      });

    return () => {
      revoked = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [id]);

  if (failed) {
    return (
      <div className={cn('flex items-center justify-center text-xs text-[var(--text-muted)]', className)}>
        <span>图片加载失败</span>
      </div>
    );
  }
  if (!url) {
    return <div className={cn('skeleton rounded-lg', className)} />;
  }
  return (
    <img
      src={url}
      alt="剪切板图片"
      onClick={onClick}
      className={cn('cursor-zoom-in object-contain', className)}
      loading="lazy"
    />
  );
}

/** 文件类型图标 */
function FileIcon() {
  return (
    <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <polyline points="14 2 14 8 20 8" />
    </svg>
  );
}

/** 单条剪切板卡片（历史列表用，含创建时间） */
function ClipCard({
  item,
  onCopy,
  onPreview,
  onDelete,
  onDownload,
  deleting,
}: {
  item: ClipItem;
  onCopy: (text: string) => void;
  onPreview: (item: ClipItem) => void;
  onDelete: (item: ClipItem) => void;
  onDownload: (item: ClipItem) => void;
  deleting: boolean;
}) {
  const filename = item.meta?.filename || '未命名文件';
  const textPreview = (item.text ?? '').slice(0, 240);

  return (
    <div className="group flex animate-slide-up flex-col overflow-hidden rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] transition-all duration-200 hover:border-[var(--brand-400)] hover:shadow-lg">
      {/* 内容区 */}
      <div className="flex flex-1 flex-col p-4">
        {item.type === 'text' && (
          <button
            type="button"
            onClick={() => item.text && onCopy(item.text)}
            className="min-h-[3rem] flex-1 cursor-pointer text-left"
            title="点击复制"
          >
            <p className="truncate-3 whitespace-pre-wrap break-words text-sm leading-relaxed text-[var(--text-primary)] transition-colors group-hover:text-[var(--brand-400)]">
              {sanitizeText(textPreview)}
            </p>
            {(item.text ?? '').length > 240 && (
              <span className="mt-1 block text-xs text-[var(--text-muted)]">
                共 {item.text?.length ?? 0} 字符
              </span>
            )}
          </button>
        )}

        {item.type === 'image' && (
          <button
            type="button"
            onClick={() => onPreview(item)}
            className="flex flex-1 items-center justify-center"
            title="点击放大"
          >
            <ClipImage
              id={item.id}
              className="max-h-40 w-full rounded-lg border border-[var(--border-subtle)]"
            />
          </button>
        )}

        {item.type === 'file' && (
          <div className="flex flex-1 items-center gap-3">
            <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg bg-[var(--bg-hover)] text-[var(--text-secondary)]">
              <FileIcon />
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium text-[var(--text-primary)]" title={String(filename)}>
                {sanitizeText(String(filename))}
              </p>
              {item.size > 0 && (
                <p className="mt-0.5 text-xs text-[var(--text-muted)]">{formatBytes(item.size)}</p>
              )}
            </div>
            <Button size="sm" variant="secondary" onClick={() => onDownload(item)} className="shrink-0">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline points="7 10 12 15 17 10" /><line x1="12" y1="15" x2="12" y2="3" /></svg>
              下载
            </Button>
          </div>
        )}
      </div>

      {/* 底部信息 */}
      <div className="flex items-center justify-between border-t border-[var(--border-subtle)] px-4 py-2.5">
        <div className="flex items-center gap-2">
          <Badge variant={typeVariant(item.type)}>{typeLabel(item.type)}</Badge>
          <span className="text-xs text-[var(--text-muted)]" title={formatTime(item.created_at)}>
            {timeAgo(item.created_at)}
          </span>
        </div>

        <div className="flex items-center gap-1">
          {item.type === 'text' && (
            <button
              type="button"
              onClick={() => item.text && onCopy(item.text)}
              className="rounded-md p-1.5 text-[var(--text-muted)] opacity-0 transition hover:bg-[var(--bg-hover)] hover:text-[var(--brand-400)] focus-ring group-hover:opacity-100"
              title="复制"
              aria-label="复制"
            >
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" /></svg>
            </button>
          )}
          {item.type === 'image' && (
            <button
              type="button"
              onClick={() => onDownload(item)}
              className="rounded-md p-1.5 text-[var(--text-muted)] opacity-0 transition hover:bg-[var(--bg-hover)] hover:text-[var(--brand-400)] focus-ring group-hover:opacity-100"
              title="下载"
              aria-label="下载"
            >
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline points="7 10 12 15 17 10" /><line x1="12" y1="15" x2="12" y2="3" /></svg>
            </button>
          )}
          <button
            type="button"
            onClick={() => onDelete(item)}
            disabled={deleting}
            className="rounded-md p-1.5 text-[var(--text-muted)] opacity-0 transition hover:bg-[var(--danger)]/10 hover:text-[var(--danger)] focus-ring group-hover:opacity-100 disabled:opacity-50"
            title="删除"
            aria-label="删除"
          >
            {deleting ? (
              <Spinner size="sm" className="!h-[15px] !w-[15px] !border" />
            ) : (
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <polyline points="3 6 5 6 21 6" />
                <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                <path d="M10 11v6M14 11v6" />
                <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
              </svg>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

/** 骨架卡片 */
function ClipCardSkeleton() {
  return (
    <div className="flex flex-col overflow-hidden rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)]">
      <div className="flex flex-1 flex-col p-4">
        <Skeleton className="h-3 w-full" />
        <Skeleton className="mt-2 h-3 w-4/5" />
        <Skeleton className="mt-2 h-3 w-3/5" />
      </div>
      <div className="flex items-center justify-between border-t border-[var(--border-subtle)] px-4 py-2.5">
        <Skeleton className="h-4 w-14 rounded-full" />
        <Skeleton className="h-4 w-10" />
      </div>
    </div>
  );
}

/**
 * 历史记录页面
 * - 顶部类型过滤标签 + 搜索框
 * - 响应式网格列表（与剪切板页面卡片样式一致）
 * - 游标分页（基于 before 参数加载更多）
 * - 客户端搜索过滤 + WS 实时同步新条目
 */
export default function History() {
  const { user, loading } = useAuth();
  const toast = useToast();

  const [items, setItems] = useState<ClipItem[]>([]);
  const [cursor, setCursor] = useState<number | null>(null);
  const [hasMore, setHasMore] = useState(true);
  const [type, setType] = useState<TypeFilter>('all');
  const [query, setQuery] = useState('');
  const [loadingFirst, setLoadingFirst] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [deletingId, setDeletingId] = useState<number | null>(null);
  const [previewItem, setPreviewItem] = useState<ClipItem | null>(null);

  const itemsRef = useRef<ClipItem[]>(items);
  itemsRef.current = items;
  const typeRef = useRef<TypeFilter>(type);
  typeRef.current = type;

  /** 拉取首页（类型变化时重置） */
  const fetchFirst = useCallback(
    async (filter: TypeFilter) => {
      setLoadingFirst(true);
      setItems([]);
      setCursor(null);
      setHasMore(true);
      try {
        const res = await api.get<ClipListResponse>('/api/clip', {
          limit: PAGE_SIZE,
          ...(filter !== 'all' ? { type: filter } : {}),
        });
        const list = res.items ?? [];
        setItems(list);
        setCursor(res.cursor ?? null);
        setHasMore(list.length >= PAGE_SIZE);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载历史失败');
      } finally {
        setLoadingFirst(false);
      }
    },
    [toast],
  );

  /** 加载更多（游标分页） */
  const fetchMore = useCallback(async () => {
    if (!cursor || !hasMore || loadingMore) return;
    setLoadingMore(true);
    try {
      const res = await api.get<ClipListResponse>('/api/clip', {
        limit: PAGE_SIZE,
        before: cursor,
        ...(type !== 'all' ? { type } : {}),
      });
      const list = res.items ?? [];
      setItems((prev) => [...prev, ...list]);
      setCursor(res.cursor ?? null);
      setHasMore(list.length >= PAGE_SIZE);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载更多失败');
    } finally {
      setLoadingMore(false);
    }
  }, [cursor, hasMore, loadingMore, type, toast]);

  // 类型切换 → 重新拉首页
  useEffect(() => {
    if (!user) return;
    fetchFirst(type);
  }, [user, type, fetchFirst]);

  // WS 连接：实时同步新增/删除
  useEffect(() => {
    if (!user) return;

    wsClient.onClipCreated = (item: ClipItem) => {
      // 仅当符合当前类型过滤时插入顶部（去重）
      const filter = typeRef.current;
      if (filter !== 'all' && item.type !== filter) return;
      setItems((prev) => {
        if (prev.some((it) => it.id === item.id)) return prev;
        return [item, ...prev];
      });
    };
    wsClient.onClipDeleted = (id: number) => {
      setItems((prev) => prev.filter((it) => it.id !== id));
    };
    wsClient.onConnected = () => {
      const maxId = itemsRef.current.reduce((m, it) => Math.max(m, it.id), 0);
      wsClient.sync(maxId);
    };
    wsClient.onSyncResult = (result) => {
      const filter = typeRef.current;
      setItems((prev) => {
        const existing = new Set(prev.map((it) => it.id));
        const fresh = result.items
          .filter((it) => !existing.has(it.id))
          .filter((it) => filter === 'all' || it.type === filter);
        if (fresh.length === 0) return prev;
        const sorted = fresh.sort((a, b) => b.id - a.id);
        return [...sorted, ...prev];
      });
    };

    wsClient.connect(authToken());

    return () => {
      wsClient.disconnect();
      wsClient.onClipCreated = () => {};
      wsClient.onClipDeleted = () => {};
      wsClient.onConnected = () => {};
      wsClient.onSyncResult = () => {};
    };
  }, [user]);

  const removeById = useCallback((id: number) => {
    setItems((prev) => prev.filter((it) => it.id !== id));
  }, []);

  const handleCopy = async (content: string) => {
    const ok = await copyToClipboard(content);
    if (ok) toast.success('已复制到剪切板');
    else toast.error('复制失败');
  };

  const handleDownload = async (item: ClipItem) => {
    const filename = item.meta?.filename || `clip-${item.id}`;
    try {
      await downloadClipContent(item.id, String(filename));
      toast.success('下载已开始');
    } catch {
      toast.error('下载失败');
    }
  };

  const handleDelete = async (item: ClipItem) => {
    setDeletingId(item.id);
    try {
      await api.del<{ status: string }>(`/api/clip/${item.id}`);
      removeById(item.id);
      toast.success('已删除');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除失败');
    } finally {
      setDeletingId(null);
    }
  };

  // 客户端搜索：对已加载条目过滤
  const filteredItems = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items;
    return items.filter((it) => {
      if (it.type === 'text') return (it.text ?? '').toLowerCase().includes(q);
      const fn = it.meta?.filename ? String(it.meta.filename) : '';
      return fn.toLowerCase().includes(q);
    });
  }, [items, query]);

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    );
  }
  if (!user) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="space-y-8">
      {/* 标题 + 搜索 */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-lg font-bold text-[var(--text-primary)]">历史记录</h1>
          <p className="mt-0.5 text-xs text-[var(--text-muted)]">浏览全部剪切板条目</p>
        </div>
        <div className="relative sm:w-72">
          <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" /></svg>
          </span>
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索已加载的文本 / 文件名…"
            className="pl-9"
          />
        </div>
      </div>

      {/* 类型过滤标签 */}
      <div className="flex flex-wrap gap-2">
        {TYPE_TABS.map((tab) => (
          <button
            key={tab.key}
            type="button"
            onClick={() => setType(tab.key)}
            className={cn(
              'rounded-full px-4 py-1.5 text-sm font-medium transition-all duration-200 focus-ring',
              type === tab.key
                ? 'text-white shadow-sm'
                : 'bg-[var(--bg-surface)] text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)]',
            )}
            style={type === tab.key ? { backgroundColor: 'var(--brand-600)' } : undefined}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* 列表 */}
      {loadingFirst ? (
        <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <ClipCardSkeleton key={i} />
          ))}
        </div>
      ) : filteredItems.length === 0 ? (
        <EmptyState
          icon={
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M3 3v5h5" /><path d="M3.05 13A9 9 0 1 0 6 5.3L3 8" /><path d="M12 7v5l4 2" /></svg>
          }
          title={query ? '没有匹配的条目' : '暂无历史记录'}
          description={query ? '尝试更换关键词或清空搜索' : '发送文本或上传文件后会出现在这里'}
        />
      ) : (
        <>
          <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {filteredItems.map((item) => (
              <ClipCard
                key={item.id}
                item={item}
                onCopy={handleCopy}
                onPreview={setPreviewItem}
                onDelete={handleDelete}
                onDownload={handleDownload}
                deleting={deletingId === item.id}
              />
            ))}
          </div>

          {/* 加载更多 */}
          <div className="flex justify-center pt-2">
            {hasMore ? (
              <Button variant="secondary" onClick={fetchMore} loading={loadingMore} disabled={loadingMore}>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="6 9 12 15 18 9" /></svg>
                加载更多
              </Button>
            ) : (
              <span className="text-xs text-[var(--text-muted)]">没有更多了</span>
            )}
          </div>
        </>
      )}

      {/* 图片放大预览 */}
      <Modal open={!!previewItem} onClose={() => setPreviewItem(null)} title="图片预览" size="lg">
        {previewItem && (
          <div className="flex flex-col items-center gap-4">
            <ClipImage
              id={previewItem.id}
              className="max-h-[70vh] max-w-full rounded-lg"
            />
            <div className="flex gap-2">
              <Button variant="secondary" onClick={() => handleDownload(previewItem)}>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline points="7 10 12 15 17 10" /><line x1="12" y1="15" x2="12" y2="3" /></svg>
                下载
              </Button>
              <Button variant="ghost" onClick={() => setPreviewItem(null)}>关闭</Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}

