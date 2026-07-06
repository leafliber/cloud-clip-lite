import { useCallback, useEffect, useRef, useState, type DragEvent } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from '@/lib/auth';
import { wsClient } from '@/lib/ws';
import { api, tokenStorage } from '@/lib/api';
import {
  sanitizeText,
  formatBytes,
  timeAgo,
  copyToClipboard,
  downloadBlob,
  cn,
} from '@/lib/security';
import {
  Button,
  Textarea,
  Card,
  Spinner,
  EmptyState,
  Modal,
  Badge,
  Skeleton,
  useToast,
} from '@/components/ui';
import type { ClipItem } from '@/types';

const MAX_DISPLAY = 10;

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
  // downloadBlob 会在下载后自动回收 blob: URL
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

/** 单条剪切板卡片 */
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
    <Card hover className="group flex animate-slide-up flex-col overflow-hidden">
      {/* 内容区 */}
      <div className="flex flex-1 flex-col p-5">
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
              className="max-h-44 w-full rounded-lg border border-[var(--border-subtle)]"
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
      <div className="flex items-center justify-between border-t border-[var(--border-subtle)] px-5 py-3">
        <div className="flex items-center gap-2">
          <Badge variant={typeVariant(item.type)}>{typeLabel(item.type)}</Badge>
          <span className="text-xs text-[var(--text-muted)]" title={item.created_at}>
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
    </Card>
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
 * 剪切板主视图
 * - 顶部文本输入 + 发送（Ctrl/⌘ + Enter 快捷发送）
 * - 文件拖拽上传区
 * - 最新 10 条响应式网格，WS 实时更新
 */
export default function Clipboard() {
  const { user, loading } = useAuth();
  const toast = useToast();

  const [text, setText] = useState('');
  const [items, setItems] = useState<ClipItem[]>([]);
  const [sending, setSending] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [initialLoading, setInitialLoading] = useState(true);
  const [dragOver, setDragOver] = useState(false);
  const [deletingId, setDeletingId] = useState<number | null>(null);
  const [previewItem, setPreviewItem] = useState<ClipItem | null>(null);

  const fileInputRef = useRef<HTMLInputElement | null>(null);

  // 保持最新的 items 引用，供 WS 回调使用，避免闭包陈旧
  const itemsRef = useRef<ClipItem[]>(items);
  itemsRef.current = items;

  /** 将新条目插入顶部（去重）并截断到最大显示数 */
  const upsertToFront = useCallback((item: ClipItem) => {
    setItems((prev) => {
      if (prev.some((it) => it.id === item.id)) return prev;
      return [item, ...prev].slice(0, MAX_DISPLAY);
    });
  }, []);

  /** 删除指定 id */
  const removeById = useCallback((id: number) => {
    setItems((prev) => prev.filter((it) => it.id !== id));
  }, []);

  // 加载最新条目 + 建立 WS 连接
  useEffect(() => {
    if (!user) return;
    let cancelled = false;

    (async () => {
      try {
        const res = await api.get<{ items: ClipItem[] }>('/api/clip', { limit: MAX_DISPLAY });
        if (cancelled) return;
        setItems(res.items ?? []);
      } catch {
        if (!cancelled) toast.error('加载剪切板失败');
      } finally {
        if (!cancelled) setInitialLoading(false);
      }
    })();

    // 注册 WS 回调
    wsClient.onClipCreated = (item: ClipItem) => {
      upsertToFront(item);
    };
    wsClient.onClipDeleted = (id: number) => {
      removeById(id);
    };
    // 连接建立后请求增量同步，补齐初始加载与连接之间可能遗漏的条目
    wsClient.onConnected = () => {
      const maxId = itemsRef.current.reduce((m, it) => Math.max(m, it.id), 0);
      wsClient.sync(maxId);
    };
    wsClient.onSyncResult = (result) => {
      setItems((prev) => {
        const existing = new Set(prev.map((it) => it.id));
        const fresh = result.items.filter((it) => !existing.has(it.id));
        if (fresh.length === 0) return prev;
        const sorted = fresh.sort((a, b) => b.id - a.id);
        return [...sorted, ...prev].slice(0, MAX_DISPLAY);
      });
    };

    wsClient.connect(authToken());

    return () => {
      cancelled = true;
      wsClient.disconnect();
      // 清空回调，避免组件卸载后仍触发状态更新（使用 no-op 兼容非可选回调类型）
      wsClient.onClipCreated = () => {};
      wsClient.onClipDeleted = () => {};
      wsClient.onConnected = () => {};
      wsClient.onSyncResult = () => {};
    };
  }, [user, upsertToFront, removeById, toast]);

  /** 发送文本 */
  const handleSendText = async () => {
    const content = text.trim();
    if (!content) return;
    setSending(true);
    try {
      const created = await api.post<ClipItem>('/api/clip', { type: 'text', text: content });
      // 乐观插入（WS 也会推送，upsert 去重）
      upsertToFront(created);
      setText('');
      toast.success('已发送');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '发送失败');
    } finally {
      setSending(false);
    }
  };

  /** 上传文件（拖拽或选择） */
  const handleUploadFiles = async (files: FileList | File[]) => {
    const list = Array.from(files);
    if (list.length === 0) return;
    setUploading(true);
    let ok = 0;
    for (const file of list) {
      try {
        const fd = new FormData();
        fd.append('file', file);
        const created = await api.post<ClipItem>('/api/clip', fd, true);
        upsertToFront(created);
        ok++;
      } catch (err) {
        toast.error(`${file.name} 上传失败：${err instanceof Error ? err.message : '未知错误'}`);
      }
    }
    if (ok > 0) toast.success(`已上传 ${ok} 个文件`);
    setUploading(false);
  };

  const handleDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setDragOver(false);
    if (e.dataTransfer.files?.length) {
      handleUploadFiles(e.dataTransfer.files);
    }
  };

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
      {/* 文本快速发送 */}
      <Card className="animate-slide-up">
        <div className="space-y-4 p-5 sm:p-6">
          <Textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="粘贴或输入文本，发送到剪切板…"
            rows={3}
            disabled={sending}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
                e.preventDefault();
                handleSendText();
              }
            }}
          />
          <div className="flex items-center justify-between">
            <span className="flex items-center gap-1.5 text-xs text-[var(--text-muted)]">
              <kbd className="rounded border border-[var(--border-default)] bg-[var(--bg-hover)] px-1.5 py-0.5 font-sans text-[10px] text-[var(--text-secondary)]">Ctrl/⌘</kbd>
              <span>+</span>
              <kbd className="rounded border border-[var(--border-default)] bg-[var(--bg-hover)] px-1.5 py-0.5 font-sans text-[10px] text-[var(--text-secondary)]">Enter</kbd>
              <span className="ml-1">快速发送</span>
            </span>
            <Button
              variant="primary"
              onClick={handleSendText}
              loading={sending}
              disabled={sending || !text.trim()}
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="22" y1="2" x2="11" y2="13" /><polygon points="22 2 15 22 11 13 2 9 22 2" /></svg>
              发送
            </Button>
          </div>
        </div>
      </Card>

      {/* 文件拖拽上传 */}
      <div
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={handleDrop}
        onClick={() => fileInputRef.current?.click()}
        className={cn(
          'flex cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed px-6 py-10 text-center transition-all duration-200',
          dragOver
            ? 'border-[var(--brand-500)] bg-[var(--brand-500)]/10 scale-[1.01]'
            : 'border-[var(--border-default)] bg-[var(--bg-surface)]/40 hover:border-[var(--brand-400)] hover:bg-[var(--bg-hover)]',
        )}
      >
        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            if (e.target.files?.length) handleUploadFiles(e.target.files);
            e.target.value = '';
          }}
        />
        {uploading ? (
          <div className="flex items-center gap-2 text-sm text-[var(--text-secondary)]">
            <Spinner size="sm" /> 上传中…
          </div>
        ) : (
          <>
            <div
              className={cn(
                'mb-3 flex h-12 w-12 items-center justify-center rounded-xl transition-colors',
                dragOver ? 'text-[var(--brand-500)]' : 'text-[var(--text-muted)]',
              )}
            >
              <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                <polyline points="17 8 12 3 7 8" />
                <line x1="12" y1="3" x2="12" y2="15" />
              </svg>
            </div>
            <p className="text-sm font-medium text-[var(--text-primary)]">
              {dragOver ? '释放以上传文件' : '拖拽文件到此处上传，或点击选择文件'}
            </p>
            <p className="mt-1 text-xs text-[var(--text-muted)]">支持图片与任意文件，可多选</p>
          </>
        )}
      </div>

      {/* 最新条目列表 */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-base font-semibold text-[var(--text-primary)]">最新条目</h2>
          <span className="text-xs text-[var(--text-muted)]">最多显示 {MAX_DISPLAY} 条</span>
        </div>

        {initialLoading ? (
          <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <ClipCardSkeleton key={i} />
            ))}
          </div>
        ) : items.length === 0 ? (
          <EmptyState
            icon={
              <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2" /><rect x="8" y="2" width="8" height="4" rx="1" /></svg>
            }
            title="还没有剪切板内容"
            description="发送文本或上传文件后会显示在这里"
          />
        ) : (
          <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {items.map((item) => (
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
        )}
      </div>

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
