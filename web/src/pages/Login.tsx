import { useEffect, useState, type FormEvent } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useAuth } from '@/lib/auth';
import { Button, Input } from '@/components/ui';

/**
 * 登录页面
 * - 桌面端：左右分栏，左侧品牌展示区（渐变背景 + Logo + 标语），右侧登录表单
 * - 移动端：单列布局，顶部 Logo + 标题
 * - 用户名 + 密码，Enter 提交，loading 态
 * - 登录成功后跳转 /
 * - 底部注册链接（当注册模式非 closed 时显示）
 */

/** 品牌渐变背景 */
const BRAND_GRADIENT = 'linear-gradient(135deg, var(--brand-600), var(--brand-700))';

/** 剪切板 Logo 图标 */
function ClipLogo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2" />
      <rect x="8" y="2" width="8" height="4" rx="1" />
    </svg>
  );
}

/**
 * 探测注册模式（best-effort）。
 * 后端若提供公开配置接口则据此显隐注册入口；探测失败时默认显示入口，
 * 以保证 open / invite 模式下注册链接可见，graceful 降级。
 */
async function probeAllowRegister(): Promise<boolean> {
  try {
    const res = await fetch('/api/public/config');
    if (!res.ok) return true;
    const data = await res.json();
    return data?.allow_register !== 'closed';
  } catch {
    return true;
  }
}

export default function Login() {
  const { login, loading, user } = useAuth();
  const navigate = useNavigate();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [allowRegister, setAllowRegister] = useState(true);

  // 已登录则跳转首页
  useEffect(() => {
    if (user) navigate('/', { replace: true });
  }, [user, navigate]);

  // 探测注册模式
  useEffect(() => {
    let cancelled = false;
    probeAllowRegister().then((v) => {
      if (!cancelled) setAllowRegister(v);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');

    if (!username.trim() || !password) {
      setError('请输入用户名和密码');
      return;
    }

    setSubmitting(true);
    try {
      await login(username.trim(), password);
      navigate('/', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败，请检查用户名和密码');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen bg-[var(--bg-base)] lg:grid lg:grid-cols-2">
      {/* 左侧品牌展示区（仅桌面端） */}
      <div
        className="relative hidden flex-col justify-between overflow-hidden p-12 text-white lg:flex"
        style={{ background: BRAND_GRADIENT }}
      >
        {/* 装饰光晕 */}
        <div className="pointer-events-none absolute -right-20 -top-20 h-72 w-72 rounded-full bg-white/10 blur-3xl" />
        <div className="pointer-events-none absolute -bottom-24 -left-16 h-80 w-80 rounded-full bg-black/10 blur-3xl" />

        {/* Logo */}
        <div className="relative flex items-center gap-3">
          <div className="flex h-11 w-11 items-center justify-center rounded-2xl bg-white/15 backdrop-blur-sm">
            <ClipLogo size={24} />
          </div>
          <span className="text-xl font-bold tracking-tight">Cloud Clip</span>
        </div>

        {/* 标语 */}
        <div className="relative max-w-md">
          <h1 className="text-4xl font-bold leading-tight">
            轻量云剪切板
            <br />
            跨设备即时同步
          </h1>
          <p className="mt-4 text-base leading-relaxed text-white/80">
            文本、图片、文件一键上传，多端实时同步。安全加密传输，让复制粘贴变得简单高效。
          </p>

          <div className="mt-8 space-y-3">
            {[
              { title: '实时同步', desc: 'WebSocket 推送，毫秒级到达' },
              { title: '安全可靠', desc: 'JWT 鉴权，端到端加密传输' },
              { title: '多端通用', desc: '浏览器、命令行、API 随处可用' },
            ].map((f) => (
              <div key={f.title} className="flex items-start gap-3">
                <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-white/20">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                    <polyline points="20 6 9 17 4 12" />
                  </svg>
                </span>
                <div>
                  <p className="text-sm font-semibold">{f.title}</p>
                  <p className="text-xs text-white/70">{f.desc}</p>
                </div>
              </div>
            ))}
          </div>
        </div>

        <p className="relative text-xs text-white/60">© Cloud Clip · 轻量云剪切板</p>
      </div>

      {/* 右侧表单区 */}
      <div className="flex min-h-screen items-center justify-center px-4 py-10 sm:px-8">
        <div className="w-full max-w-md animate-slide-up">
          {/* 移动端 Logo + 标题 */}
          <div className="mb-8 flex flex-col items-center text-center lg:hidden">
            <div
              className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl text-white"
              style={{ background: BRAND_GRADIENT }}
            >
              <ClipLogo size={28} />
            </div>
            <h1 className="text-2xl font-bold tracking-tight text-[var(--text-primary)]">Cloud Clip</h1>
            <p className="mt-1 text-sm text-[var(--text-muted)]">登录以开始使用</p>
          </div>

          {/* 桌面端标题 */}
          <div className="mb-8 hidden lg:block">
            <h2 className="text-2xl font-bold tracking-tight text-[var(--text-primary)]">欢迎回来</h2>
            <p className="mt-1 text-sm text-[var(--text-secondary)]">登录你的账号以继续</p>
          </div>

          <form onSubmit={handleSubmit} className="space-y-5">
            {error && (
              <div
                role="alert"
                className="flex items-start gap-2 rounded-lg border border-[var(--danger)]/30 bg-[var(--danger)]/10 px-3.5 py-2.5 text-sm text-[var(--danger)] animate-fade-in"
              >
                <svg className="mt-0.5 shrink-0" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="12" cy="12" r="10" />
                  <line x1="12" y1="8" x2="12" y2="12" />
                  <line x1="12" y1="16" x2="12.01" y2="16" />
                </svg>
                <span>{error}</span>
              </div>
            )}

            <Input
              id="login-username"
              name="username"
              label="用户名"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="请输入用户名"
              autoComplete="username"
              autoFocus
              disabled={submitting}
            />

            <Input
              id="login-password"
              name="password"
              label="密码"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="请输入密码"
              autoComplete="current-password"
              disabled={submitting}
            />

            <Button
              type="submit"
              variant="primary"
              size="lg"
              className="w-full"
              loading={submitting || loading}
              disabled={submitting}
            >
              登录
            </Button>
          </form>

          {/* 注册入口 */}
          {allowRegister && (
            <p className="mt-8 text-center text-sm text-[var(--text-secondary)] animate-fade-in">
              还没有账号？
              <Link
                to="/register"
                className="ml-1 font-medium text-[var(--brand-400)] transition-colors hover:text-[var(--brand-500)] hover:underline"
              >
                立即注册
              </Link>
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
