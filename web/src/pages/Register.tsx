import { useEffect, useState, type FormEvent } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useAuth } from '@/lib/auth';
import { Button, Input } from '@/components/ui';

/**
 * 注册页面
 * - 与登录页类似的双栏布局
 * - 表单：用户名 + 密码 + 确认密码 + 邀请码（可选）
 * - 前端校验：用户名 3-32 字符，密码 ≥ 8，两次密码一致
 * - 注册成功自动登录跳转 /
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
 * closed 模式下后端会拒绝注册；此处探测公开配置以决定是否展示注册表单，
 * 探测失败时默认放行（由后端兜底校验），graceful 降级。
 */
async function probeAllowRegister(): Promise<string> {
  try {
    const res = await fetch('/api/public/config');
    if (!res.ok) return 'open';
    const data = await res.json();
    const mode = data?.allow_register;
    return mode === 'closed' || mode === 'invite' || mode === 'open' ? mode : 'open';
  } catch {
    return 'open';
  }
}

export default function Register() {
  const { register, user } = useAuth();
  const navigate = useNavigate();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [inviteCode, setInviteCode] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [registerMode, setRegisterMode] = useState<string>('open');

  // 已登录则跳转首页
  useEffect(() => {
    if (user) navigate('/', { replace: true });
  }, [user, navigate]);

  // 探测注册模式
  useEffect(() => {
    let cancelled = false;
    probeAllowRegister().then((m) => {
      if (!cancelled) setRegisterMode(m);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const inviteRequired = registerMode === 'invite';

  const validate = (): string => {
    if (username.trim().length < 3 || username.trim().length > 32) {
      return '用户名长度需 3-32 字符';
    }
    if (!/^[a-zA-Z0-9_-]+$/.test(username.trim())) {
      return '用户名只能包含字母、数字、下划线和连字符';
    }
    if (password.length < 8) {
      return '密码长度至少 8 字符';
    }
    if (password.length > 128) {
      return '密码长度不能超过 128 字符';
    }
    if (password !== confirmPassword) {
      return '两次输入的密码不一致';
    }
    if (inviteRequired && !inviteCode.trim()) {
      return '当前为邀请码注册模式，请填写邀请码';
    }
    return '';
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');

    const msg = validate();
    if (msg) {
      setError(msg);
      return;
    }

    setSubmitting(true);
    try {
      const code = inviteCode.trim();
      await register(username.trim(), password, code || undefined);
      // 注册成功后自动登录，跳转首页
      navigate('/', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : '注册失败，请稍后重试');
    } finally {
      setSubmitting(false);
    }
  };

  // 注册关闭：展示提示与返回登录入口
  if (registerMode === 'closed') {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[var(--bg-base)] px-4 py-10">
        <div className="w-full max-w-md animate-slide-up text-center">
          <div
            className="mx-auto mb-6 flex h-14 w-14 items-center justify-center rounded-2xl text-white"
            style={{ background: BRAND_GRADIENT }}
          >
            <ClipLogo size={28} />
          </div>
          <h1 className="text-xl font-bold text-[var(--text-primary)]">注册已关闭</h1>
          <p className="mt-2 text-sm text-[var(--text-secondary)]">
            当前系统已关闭注册，请联系管理员创建账号。
          </p>
          <div className="mt-6">
            <Link
              to="/login"
              className="inline-flex items-center gap-1.5 text-sm font-medium text-[var(--brand-400)] transition-colors hover:text-[var(--brand-500)] hover:underline"
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="19" y1="12" x2="5" y2="12" /><polyline points="12 19 5 12 12 5" /></svg>
              返回登录
            </Link>
          </div>
        </div>
      </div>
    );
  }

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
            加入 Cloud Clip
            <br />
            开启高效协作
          </h1>
          <p className="mt-4 text-base leading-relaxed text-white/80">
            创建账号，即刻享受跨设备剪切板同步。文本、图片、文件，一切尽在指尖流转。
          </p>

          <div className="mt-8 space-y-3">
            {[
              { title: '免费上手', desc: '注册即用，无需复杂配置' },
              { title: '隐私优先', desc: '密码加盐哈希，凭证本地加密存储' },
              { title: '随时管理', desc: '完整的历史记录与设备管理' },
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
            <h1 className="text-2xl font-bold tracking-tight text-[var(--text-primary)]">创建账号</h1>
            <p className="mt-1 text-sm text-[var(--text-muted)]">注册以开始使用 Cloud Clip</p>
          </div>

          {/* 桌面端标题 */}
          <div className="mb-8 hidden lg:block">
            <h2 className="text-2xl font-bold tracking-tight text-[var(--text-primary)]">创建新账号</h2>
            <p className="mt-1 text-sm text-[var(--text-secondary)]">填写以下信息完成注册</p>
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
              id="reg-username"
              name="username"
              label="用户名"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="3-32 位字母、数字、_ 或 -"
              autoComplete="username"
              autoFocus
              disabled={submitting}
              hint="3-32 个字符，支持字母、数字、下划线与连字符"
            />

            <Input
              id="reg-password"
              name="password"
              label="密码"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="至少 8 个字符"
              autoComplete="new-password"
              disabled={submitting}
              hint="至少 8 个字符"
            />

            <Input
              id="reg-confirm"
              name="confirm-password"
              label="确认密码"
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              placeholder="再次输入密码"
              autoComplete="new-password"
              disabled={submitting}
            />

            <Input
              id="reg-invite"
              name="invite-code"
              label={inviteRequired ? '邀请码' : '邀请码（可选）'}
              value={inviteCode}
              onChange={(e) => setInviteCode(e.target.value)}
              placeholder={inviteRequired ? '请输入邀请码' : '如有邀请码请填写'}
              disabled={submitting}
              hint={inviteRequired ? '当前为邀请码注册模式，邀请码必填' : undefined}
            />

            <Button
              type="submit"
              variant="primary"
              size="lg"
              className="w-full"
              loading={submitting}
              disabled={submitting}
            >
              注册
            </Button>
          </form>

          <p className="mt-8 text-center text-sm text-[var(--text-secondary)]">
            已有账号？
            <Link
              to="/login"
              className="ml-1 font-medium text-[var(--brand-400)] transition-colors hover:text-[var(--brand-500)] hover:underline"
            >
              返回登录
            </Link>
          </p>
        </div>
      </div>
    </div>
  );
}
