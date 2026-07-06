import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from 'react';
import { cn } from '../../lib/security';

/**
 * 现代化 UI 组件库
 * 使用 CSS 变量适配 light/dark 双主题
 * 通过 var(--xxx) 引用 index.css 中的设计令牌
 */

const s = {
  bgBase: 'bg-[var(--bg-base)]',
  bgElevated: 'bg-[var(--bg-elevated)]',
  bgSurface: 'bg-[var(--bg-surface)]',
  bgHover: 'hover:bg-[var(--bg-hover)]',
  bgInput: 'bg-[var(--bg-input)]',
  borderDefault: 'border-[var(--border-default)]',
  borderSubtle: 'border-[var(--border-subtle)]',
  textPrimary: 'text-[var(--text-primary)]',
  textSecondary: 'text-[var(--text-secondary)]',
  textMuted: 'text-[var(--text-muted)]',
};

/* ============================== Spinner ============================== */

export function Spinner({ size = 'md', className }: { size?: 'sm' | 'md' | 'lg'; className?: string }) {
  const sizes = { sm: 'h-4 w-4 border-2', md: 'h-5 w-5 border-2', lg: 'h-8 w-8 border-[3px]' };
  return (
    <span
      role="status"
      aria-label="加载中"
      className={cn('inline-block animate-spin rounded-full border-transparent border-t-[var(--brand-500)]', sizes[size], className)}
      style={{ borderColor: 'var(--border-default)', borderTopColor: 'var(--brand-500)' }}
    />
  );
}

/* ============================== Button ============================== */

type ButtonVariant = 'primary' | 'secondary' | 'danger' | 'ghost' | 'outline';
type ButtonSize = 'sm' | 'md' | 'lg' | 'icon';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
}

export function Button({
  variant = 'primary',
  size = 'md',
  loading = false,
  disabled,
  className,
  children,
  ...rest
}: ButtonProps) {
  const base =
    'inline-flex items-center justify-center gap-2 rounded-lg font-medium transition-all duration-200 focus-ring disabled:opacity-50 disabled:cursor-not-allowed select-none active:scale-[0.97]';
  const variants: Record<ButtonVariant, string> = {
    primary: 'text-white shadow-sm hover:shadow-md',
    secondary: `${s.textPrimary} ${s.bgSurface} ${s.borderDefault} border ${s.bgHover}`,
    danger: 'bg-[var(--danger)] text-white shadow-sm hover:brightness-110',
    ghost: `bg-transparent ${s.textSecondary} ${s.bgHover} hover:${s.textPrimary}`,
    outline: `${s.borderDefault} border bg-transparent ${s.textPrimary} ${s.bgHover}`,
  };
  const sizes: Record<ButtonSize, string> = {
    sm: 'h-8 px-3 text-xs',
    md: 'h-10 px-4 text-sm',
    lg: 'h-12 px-6 text-base',
    icon: 'h-9 w-9',
  };
  const style = variant === 'primary'
    ? { backgroundColor: 'var(--brand-600)' }
    : variant === 'danger'
    ? { backgroundColor: 'var(--danger)' }
    : undefined;

  return (
    <button
      className={cn(base, variants[variant], sizes[size], className)}
      style={style}
      disabled={disabled || loading}
      {...rest}
    >
      {loading && <Spinner size="sm" />}
      {children}
    </button>
  );
}

/* ============================== Input ============================== */

interface InputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'size'> {
  label?: string;
  error?: string;
  hint?: string;
}

export function Input({ label, error, hint, className, id, ...rest }: InputProps) {
  const inputId = id || rest.name;
  return (
    <div className="w-full">
      {label && (
        <label htmlFor={inputId} className={cn('mb-1.5 block text-sm font-medium', s.textSecondary)}>
          {label}
        </label>
      )}
      <input
        id={inputId}
        className={cn(
          'w-full rounded-lg border px-3.5 py-2.5 text-sm transition-all duration-200 focus-ring',
          s.bgInput,
          s.textPrimary,
          'placeholder:text-[var(--text-muted)]',
          error ? 'border-[var(--danger)]' : cn(s.borderDefault, 'hover:border-[var(--brand-400)]'),
          className,
        )}
        {...rest}
      />
      {error && <p className="mt-1.5 text-xs text-[var(--danger)]">{error}</p>}
      {hint && !error && <p className={cn('mt-1.5 text-xs', s.textMuted)}>{hint}</p>}
    </div>
  );
}

/* ============================== Textarea ============================== */

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string;
  error?: string;
}

export function Textarea({ label, error, className, id, ...rest }: TextareaProps) {
  const inputId = id || rest.name;
  return (
    <div className="w-full">
      {label && (
        <label htmlFor={inputId} className={cn('mb-1.5 block text-sm font-medium', s.textSecondary)}>
          {label}
        </label>
      )}
      <textarea
        id={inputId}
        className={cn(
          'w-full rounded-lg border px-3.5 py-2.5 text-sm transition-all duration-200 focus-ring resize-y min-h-[80px]',
          s.bgInput,
          s.textPrimary,
          'placeholder:text-[var(--text-muted)]',
          error ? 'border-[var(--danger)]' : cn(s.borderDefault, 'hover:border-[var(--brand-400)]'),
          className,
        )}
        {...rest}
      />
      {error && <p className="mt-1.5 text-xs text-[var(--danger)]">{error}</p>}
    </div>
  );
}

/* ============================== Select ============================== */

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  error?: string;
  options: { value: string; label: string }[];
}

export function Select({ label, error, options, className, id, ...rest }: SelectProps) {
  const selectId = id || rest.name;
  return (
    <div className="w-full">
      {label && (
        <label htmlFor={selectId} className={cn('mb-1.5 block text-sm font-medium', s.textSecondary)}>
          {label}
        </label>
      )}
      <select
        id={selectId}
        className={cn(
          'w-full rounded-lg border px-3.5 py-2.5 text-sm transition-all duration-200 focus-ring cursor-pointer',
          s.bgInput,
          s.textPrimary,
          s.borderDefault,
          className,
        )}
        {...rest}
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>{opt.label}</option>
        ))}
      </select>
      {error && <p className="mt-1.5 text-xs text-[var(--danger)]">{error}</p>}
    </div>
  );
}

/* ============================== Card ============================== */

export function Card({ className, children, hover }: { className?: string; children: ReactNode; hover?: boolean }) {
  return (
    <div
      className={cn(
        'rounded-xl border transition-all duration-200',
        s.bgSurface,
        s.borderDefault,
        hover && 'hover:border-[var(--brand-400)] hover:shadow-lg cursor-pointer',
        className,
      )}
    >
      {children}
    </div>
  );
}

export function CardHeader({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div className={cn('flex items-center justify-between border-b px-6 py-4', s.borderSubtle, className)}>
      {children}
    </div>
  );
}

export function CardBody({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('p-6', className)}>{children}</div>;
}

export function CardTitle({ className, children }: { className?: string; children: ReactNode }) {
  return <h3 className={cn('text-base font-semibold', s.textPrimary, className)}>{children}</h3>;
}

/* ============================== Badge ============================== */

type BadgeVariant = 'success' | 'warning' | 'danger' | 'info' | 'default';

export function Badge({ variant = 'info', className, children }: { variant?: BadgeVariant; className?: string; children: ReactNode }) {
  const colorMap: Record<BadgeVariant, string> = {
    success: 'text-[var(--success)]',
    warning: 'text-[var(--warning)]',
    danger: 'text-[var(--danger)]',
    info: 'text-[var(--brand-400)]',
    default: s.textSecondary,
  };
  const bgMap: Record<BadgeVariant, string> = {
    success: 'bg-[var(--success)]/10',
    warning: 'bg-[var(--warning)]/10',
    danger: 'bg-[var(--danger)]/10',
    info: 'bg-[var(--brand-500)]/10',
    default: s.bgHover,
  };
  return (
    <span className={cn('inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium', bgMap[variant], colorMap[variant], className)}>
      {children}
    </span>
  );
}

/* ============================== Modal ============================== */

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title?: ReactNode;
  children: ReactNode;
  className?: string;
  closeOnBackdrop?: boolean;
  size?: 'sm' | 'md' | 'lg' | 'xl';
}

export function Modal({ open, onClose, title, children, className, closeOnBackdrop = true, size = 'md' }: ModalProps) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose]);

  if (!open) return null;

  const maxW = { sm: 'max-w-sm', md: 'max-w-lg', lg: 'max-w-2xl', xl: 'max-w-4xl' }[size];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 animate-fade-in" onMouseDown={(e) => { if (closeOnBackdrop && e.target === e.currentTarget) onClose(); }}>
      <div className="absolute inset-0 bg-black/50 backdrop-blur-sm" aria-hidden="true" />
      <div
        role="dialog"
        aria-modal="true"
        className={cn('relative z-10 w-full rounded-2xl border shadow-2xl animate-scale-in', s.bgSurface, s.borderDefault, maxW, className)}
      >
        {title && (
          <div className={cn('flex items-center justify-between border-b px-6 py-4', s.borderSubtle)}>
            <h2 className={cn('text-lg font-semibold', s.textPrimary)}>{title}</h2>
            <button onClick={onClose} aria-label="关闭" className={cn('rounded-lg p-1.5 transition-colors', s.textMuted, s.bgHover, 'hover:' + s.textPrimary)}>
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg>
            </button>
          </div>
        )}
        <div className="p-6">{children}</div>
      </div>
    </div>
  );
}

/* ============================== ConfirmDialog ============================== */

export function ConfirmDialog({
  open, title = '确认操作', message, confirmText = '确认', cancelText = '取消',
  variant = 'primary', loading = false, onConfirm, onCancel,
}: {
  open: boolean; title?: string; message: ReactNode; confirmText?: string; cancelText?: string;
  variant?: 'danger' | 'primary' | 'default'; loading?: boolean; onConfirm: () => void; onCancel: () => void;
}) {
  return (
    <Modal open={open} onClose={onCancel} title={title} size="sm">
      <div className="space-y-5">
        <div className={cn('text-sm', s.textSecondary)}>{message}</div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onCancel} disabled={loading}>{cancelText}</Button>
          <Button variant={variant === 'danger' ? 'danger' : 'primary'} size="sm" loading={loading} onClick={onConfirm}>{confirmText}</Button>
        </div>
      </div>
    </Modal>
  );
}

/* ============================== EmptyState ============================== */

export function EmptyState({ icon, title, description, action, className }: { icon?: ReactNode; title: string; description?: ReactNode; action?: ReactNode; className?: string }) {
  return (
    <div className={cn('flex flex-col items-center justify-center py-16 text-center', className)}>
      {icon && <div className={cn('mb-4 flex h-16 w-16 items-center justify-center rounded-2xl', s.bgHover, s.textMuted)}>{icon}</div>}
      <p className={cn('text-base font-medium', s.textPrimary)}>{title}</p>
      {description && <p className={cn('mt-1.5 max-w-sm text-sm', s.textMuted)}>{description}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

/* ============================== Skeleton ============================== */

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn('skeleton rounded-lg', className)} />;
}

/* ============================== Toast ============================== */

type ToastType = 'success' | 'error' | 'info' | 'warning';
interface ToastItem { id: number; type: ToastType; message: string; }
interface ToastApi { show: (m: string, t?: ToastType) => void; success: (m: string) => void; error: (m: string) => void; info: (m: string) => void; warning: (m: string) => void; }
interface ToastContextValue extends ToastApi { toast: ToastApi }

const ToastContext = createContext<ToastContextValue | null>(null);
let toastId = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const remove = useCallback((id: number) => setToasts((p) => p.filter((t) => t.id !== id)), []);
  const show = useCallback((message: string, type: ToastType = 'info') => {
    const id = ++toastId;
    setToasts((p) => [...p, { id, type, message }]);
    setTimeout(() => remove(id), 3500);
  }, [remove]);

  const methods: ToastApi = {
    show, success: (m) => show(m, 'success'), error: (m) => show(m, 'error'),
    info: (m) => show(m, 'info'), warning: (m) => show(m, 'warning'),
  };
  const value: ToastContextValue = { ...methods, toast: methods };

  const icons: Record<ToastType, ReactNode> = {
    success: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12" /></svg>,
    error: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><line x1="15" y1="9" x2="9" y2="15" /><line x1="9" y1="9" x2="15" y2="15" /></svg>,
    info: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><line x1="12" y1="16" x2="12" y2="12" /><line x1="12" y1="8" x2="12.01" y2="8" /></svg>,
    warning: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" /></svg>,
  };
  const colors: Record<ToastType, string> = {
    success: 'text-[var(--success)]', error: 'text-[var(--danger)]',
    info: 'text-[var(--brand-400)]', warning: 'text-[var(--warning)]',
  };

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-80 flex-col gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            className={cn('pointer-events-auto flex items-start gap-3 rounded-xl border px-4 py-3 text-sm shadow-lg animate-slide-in-right', s.bgSurface, s.borderDefault, s.textPrimary)}
          >
            <span className={cn('mt-0.5 shrink-0', colors[t.type])}>{icons[t.type]}</span>
            <span className="flex-1 break-words">{t.message}</span>
            <button onClick={() => remove(t.id)} className={cn('shrink-0 transition-colors', s.textMuted, 'hover:' + s.textPrimary)} aria-label="关闭">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg>
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error('useToast must be used within ToastProvider');
  return ctx;
}
