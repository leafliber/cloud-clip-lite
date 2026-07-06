// 用户实体
export interface User {
  id: number;
  username: string;
  email?: string;
  role: 'user' | 'admin';
  status: 'active' | 'disabled';
  max_item_size: number;
  quota_bytes: number;
  retention_days: number;
  created_at: string;
  updated_at: string;
}

// 剪切板条目
export interface ClipItem {
  id: number;
  type: 'text' | 'image' | 'file';
  size: number;
  mime_type?: string;
  text?: string;
  has_blob?: boolean;
  sha256?: string;
  meta?: Record<string, any>;
  created_at: string;
  expires_at?: string;
}

// 设备
export interface Device {
  id: number;
  user_id: number;
  name: string;
  type: string;
  api_token_hash?: string;
  api_token?: string; // 仅创建时返回
  has_token?: boolean; // 后端返回，标识是否已配置 API Token
  last_seen_at?: string;
  created_at: string;
}

// 认证响应
export interface AuthResponse {
  access_token: string;
  refresh_token: string;
  user: User;
}

// 统一错误结构
export interface ApiError {
  error: {
    code: string;
    message: string;
    extra?: Record<string, any>;
  };
}

// 剪切板列表响应
export interface ClipListResponse {
  items: ClipItem[];
  cursor: number;
  limit: number;
}

// 设备列表响应
export interface DeviceListResponse {
  devices: Device[];
}
