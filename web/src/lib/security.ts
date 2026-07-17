/**
 * 安全与格式化工具集
 * - 渲染用户文本时依赖 React 文本节点的自动转义（textContent），无需手动转义
 * - 全项目禁止使用 dangerouslySetInnerHTML
 */

/**
 * 用户文本透传。
 *
 * React 在渲染文本节点时会自动转义 < > & " '（通过 textContent / createTextNode，
 * 浏览器不会将其解析为 HTML），因此此处无需、也不应再次手动转义——手动转义会导致
 * `&lt;` 等实体被二次转义，在界面上显示为字面量 `&lt;`。
 *
 * 保留该函数用于在调用处显式标注“此处渲染的是用户可控文本”，实质为恒等映射，
 * 安全性由 React 文本节点保证。如需移除，可直接替换为原始字符串。
 */
export function sanitizeText(text: string): string {
  if (text === null || text === undefined) return '';
  return String(text);
}

/**
 * 格式化文件大小（字节 → 人类可读）。
 */
export function formatBytes(bytes: number): string {
  if (bytes === null || bytes === undefined || isNaN(bytes) || bytes < 0) return '0 B';
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, i);
  // 整数或大值保留较少小数
  const digits = i === 0 ? 0 : value >= 100 ? 0 : value >= 10 ? 1 : 2;
  return `${value.toFixed(digits)} ${units[i]}`;
}

/**
 * 将 ISO 时间字符串格式化为 "YYYY-MM-DD HH:mm:ss"。
 */
export function formatTime(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/**
 * 相对时间，如 "3 分钟前"。
 */
export function timeAgo(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const diff = Math.max(0, Date.now() - d.getTime());
  const sec = Math.floor(diff / 1000);
  if (sec < 5) return '刚刚';
  if (sec < 60) return `${sec} 秒前`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min} 分钟前`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr} 小时前`;
  const day = Math.floor(hr / 24);
  if (day < 30) return `${day} 天前`;
  const month = Math.floor(day / 30);
  if (month < 12) return `${month} 个月前`;
  return `${Math.floor(day / 365)} 年前`;
}

/**
 * 复制文本到剪贴板。优先使用现代 Clipboard API，失败时回退到 execCommand。
 * @returns 是否复制成功
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  if (text === null || text === undefined) return false;
  const str = String(text);
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(str);
      return true;
    }
  } catch {
    // 落入回退逻辑
  }
  try {
    const ta = document.createElement('textarea');
    ta.value = str;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.top = '-9999px';
    ta.style.left = '-9999px';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    return ok;
  } catch {
    return false;
  }
}

/**
 * 复制 Blob（图片/文件二进制）到系统剪贴板。
 *
 * 使用 Clipboard API 的 ClipboardItem 写入，要求安全上下文（HTTPS / localhost）。
 * 浏览器仅支持有限的 MIME 类型（如 image/png、image/jpeg 等），不支持的类型会抛出异常，
 * 调用方应自行处理回退逻辑（如复制文件名）。
 *
 * @returns 是否复制成功
 */
export async function copyBlobToClipboard(blob: Blob): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext && typeof ClipboardItem !== 'undefined') {
      const mimeType = blob.type || 'application/octet-stream';
      const clipboardBlob = blob.type ? blob : new Blob([blob], { type: mimeType });
      const item = new ClipboardItem({ [mimeType]: clipboardBlob });
      await navigator.clipboard.write([item]);
      return true;
    }
  } catch {
    // 不支持或 MIME 类型不被浏览器允许，落入回退逻辑
  }
  return false;
}

/**
 * 安全下载：通过临时 <a> 标签触发下载，rel 防止 tabnabbing。
 * source 可为 Blob（自动创建 object URL）或字符串 URL（如 blob:/data:）。
 * 若为 blob: 链接，下载完成后自动回收。
 */
export function downloadBlob(source: Blob | string, filename: string): void {
  const isBlob = source instanceof Blob;
  const url = isBlob ? URL.createObjectURL(source) : String(source);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.rel = 'noopener noreferrer';
  a.target = '_self';
  a.style.display = 'none';
  document.body.appendChild(a);
  a.click();
  // 延迟移除节点并回收 blob URL
  window.setTimeout(() => {
    if (a.parentNode) a.parentNode.removeChild(a);
    if (url.startsWith('blob:')) {
      try {
        URL.revokeObjectURL(url);
      } catch {
        // ignore
      }
    }
  }, 200);
}

/**
 * 连接 className（简易 clsx 替代）。
 */
export function cn(...classes: Array<string | false | null | undefined>): string {
  return classes.filter(Boolean).join(' ');
}
