import { Navigate, Outlet, Route, Routes } from 'react-router-dom';
import { AuthProvider, useAuth } from './lib/auth';
import { ThemeProvider } from './lib/theme';
import { ToastProvider, Spinner } from './components/ui';
import { Layout } from './components/Layout';
import Login from './pages/Login';
import Register from './pages/Register';
import Clipboard from './pages/Clipboard';
import History from './pages/History';
import Devices from './pages/Devices';
import Settings from './pages/Settings';
import Dashboard from './pages/admin/Dashboard';
import Users from './pages/admin/Users';
import SystemSettings from './pages/admin/SystemSettings';

/**
 * 应用根组件
 * - 装配 AuthProvider / ToastProvider
 * - 定义路由：公共页（登录/注册）、受保护页（需登录）、管理员页（需 admin 角色）
 */

function FullScreenSpinner() {
  return (
    <div className="flex h-screen items-center justify-center bg-[var(--bg-base)]">
      <Spinner size="lg" />
    </div>
  );
}

/** 需要登录才能访问；未登录跳转 /login */
function RequireAuth({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();
  if (loading) return <FullScreenSpinner />;
  if (!user) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

/** 需要 admin 角色；非 admin 跳转首页 */
function RequireAdmin() {
  const { user, loading } = useAuth();
  if (loading) return <FullScreenSpinner />;
  if (!user) return <Navigate to="/login" replace />;
  if (user.role !== 'admin') return <Navigate to="/" replace />;
  return <Outlet />;
}

export default function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <ToastProvider>
          <Routes>
          {/* 公共路由 */}
          <Route path="/login" element={<Login />} />
          <Route path="/register" element={<Register />} />

          {/* 受保护路由（需登录），共享 Layout */}
          <Route
            element={
              <RequireAuth>
                <Layout />
              </RequireAuth>
            }
          >
            <Route path="/" element={<Clipboard />} />
            <Route path="/history" element={<History />} />
            <Route path="/devices" element={<Devices />} />
            <Route path="/settings" element={<Settings />} />

            {/* 管理员路由（需 admin 角色） */}
            <Route element={<RequireAdmin />}>
              <Route path="/admin" element={<Dashboard />} />
              <Route path="/admin/users" element={<Users />} />
              <Route path="/admin/settings" element={<SystemSettings />} />
            </Route>
          </Route>

          {/* 兜底 */}
          <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </ToastProvider>
      </AuthProvider>
    </ThemeProvider>
  );
}
