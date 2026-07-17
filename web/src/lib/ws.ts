import type { ClipItem } from '../types';
import { tokenStorage } from './api';

/**
 * WebSocket 客户端
 * - 自动管理 WS 连接生命周期
 * - 鉴权：通过 ?token= 参数传递 JWT
 * - 心跳：每 30s 发送 ping 消息
 * - 断线重连：指数退避（1s, 2s, 4s, 8s, ..., 上限 30s）
 * - 消息处理：connected / clip.created / clip.deleted / pong / sync.result / error
 */

/** 服务端消息结构（与后端 ws.ServerMessage 对应） */
interface ServerMessage {
  type: string;
  data?: any;
  ts?: string;
  id?: string;
}

/** 客户端发送给服务端的消息 */
interface ClientMessage {
  type: string;
  data?: unknown;
}

/** 增量同步结果 */
export interface SyncResult {
  since: number;
  items: ClipItem[];
  count: number;
}

const HEARTBEAT_INTERVAL = 30_000; // 30s
const MAX_RECONNECT_DELAY = 30_000; // 上限 30s
const RECONNECT_BASE = 1_000; // 起始 1s
const SYNC_LIMIT = 100; // 服务端单次 sync 返回上限（与后端 ws syncLimit 一致）

class WSClient {
  private ws: WebSocket | null = null;
  private token = '';
  private url: string;
  private manualClose = false;
  private reconnectAttempts = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;

  // 事件回调
  onClipCreated?: (item: ClipItem) => void;
  onClipDeleted?: (id: number) => void;
  onConnected?: () => void;
  onDisconnected?: () => void;
  onSyncResult?: (result: SyncResult) => void;
  onError?: (code: string, message: string) => void;

  constructor() {
    this.url = this.buildUrl();
  }

  /** 根据当前页面协议推导 ws/wss 地址 */
  private buildUrl(): string {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${proto}//${window.location.host}/api/ws`;
  }

  /** 建立连接 */
  connect(token: string): void {
    this.token = token;
    this.manualClose = false;
    this.reconnectAttempts = 0;
    this.clearReconnectTimer();
    this.open();
  }

  /** 主动断开，不再重连 */
  disconnect(): void {
    this.manualClose = true;
    this.stopHeartbeat();
    this.clearReconnectTimer();
    if (this.ws) {
      try {
        this.ws.onclose = null;
        this.ws.onerror = null;
        this.ws.onmessage = null;
        this.ws.onopen = null;
        this.ws.close();
      } catch {
        // ignore
      }
      this.ws = null;
    }
  }

  /** 请求增量同步（sinceId 之后的新条目） */
  sync(sinceId: number): void {
    this.send({ type: 'sync', data: { since: sinceId } });
  }

  // ---------- 内部实现 ----------

  private open(): void {
    // 每次（重）连都现读最新 token：HTTP 侧静默刷新后 localStorage 才是最新值，
    // 沿用旧 token 会导致重连握手 401 → 指数退避无限重试
    const token = tokenStorage.getAccessToken();
    if (!token) return;
    this.token = token;

    // token 通过查询参数传递（浏览器 WS 无法设置自定义头）
    const url = `${this.url}?token=${encodeURIComponent(this.token)}`;

    try {
      this.ws = new WebSocket(url);
    } catch {
      this.scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      this.reconnectAttempts = 0;
      this.startHeartbeat();
      // connected 消息由服务端在握手后下发，此处不直接触发 onConnected
    };

    this.ws.onmessage = (event) => {
      this.handleMessage(event.data);
    };

    this.ws.onerror = () => {
      // 错误后通常会触发 onclose，由其处理重连
    };

    this.ws.onclose = () => {
      this.stopHeartbeat();
      this.onDisconnected?.();
      if (!this.manualClose) {
        this.scheduleReconnect();
      }
    };
  }

  /** 处理服务端消息 */
  private handleMessage(raw: unknown): void {
    if (typeof raw !== 'string') return;
    let msg: ServerMessage;
    try {
      msg = JSON.parse(raw);
    } catch {
      return;
    }

    switch (msg.type) {
      case 'connected':
        this.onConnected?.();
        break;

      case 'clip.created': {
        const data = msg.data || {};
        const item = data.item as ClipItem | undefined;
        if (item) this.onClipCreated?.(item);
        break;
      }

      case 'clip.deleted': {
        const data = msg.data || {};
        if (typeof data.id === 'number') this.onClipDeleted?.(data.id);
        break;
      }

      case 'pong':
        // 心跳响应，无需处理
        break;

      case 'sync.result': {
        const data = msg.data || {};
        const items: ClipItem[] = Array.isArray(data.items) ? data.items : [];
        this.onSyncResult?.({
          since: data.since ?? 0,
          items,
          count: typeof data.count === 'number' ? data.count : 0,
        });
        // 服务端单次 sync 最多返回 SYNC_LIMIT 条最旧条目：满额说明 since 之后可能还有
        // 遗漏，用本批最大 id 作为新 since 继续拉取，直到不足上限为止
        if (items.length >= SYNC_LIMIT) {
          const maxId = items.reduce((m, it) => Math.max(m, it?.id ?? 0), 0);
          if (maxId > 0) this.sync(maxId);
        }
        break;
      }

      case 'error': {
        const data = msg.data || {};
        this.onError?.(data.code || 'UNKNOWN', data.message || '未知错误');
        break;
      }

      default:
        // 未知消息类型，忽略
        break;
    }
  }

  /** 启动应用层心跳 */
  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => {
      this.send({ type: 'ping' });
    }, HEARTBEAT_INTERVAL);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  /** 发送消息（仅在连接打开时） */
  private send(msg: ClientMessage): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      try {
        this.ws.send(JSON.stringify(msg));
      } catch {
        // ignore
      }
    }
  }

  /** 指数退避重连 */
  private scheduleReconnect(): void {
    if (this.manualClose) return;
    this.clearReconnectTimer();
    const delay = Math.min(
      RECONNECT_BASE * Math.pow(2, this.reconnectAttempts),
      MAX_RECONNECT_DELAY,
    );
    this.reconnectAttempts++;
    this.reconnectTimer = setTimeout(() => {
      this.open();
    }, delay);
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }
}

export const wsClient = new WSClient();
