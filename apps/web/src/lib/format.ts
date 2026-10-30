const pad = (value: number) => String(value).padStart(2, "0");

/** 与既有节点组件保持一致：显式 UTC，避免运行环境时区让同一数据两种读数。 */
export function formatDateTime(value: string | null | undefined): string | null {
  if (!value) return null;
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) return null;
  return (
    `${time.getUTCFullYear()}-${pad(time.getUTCMonth() + 1)}-${pad(time.getUTCDate())} ` +
    `${pad(time.getUTCHours())}:${pad(time.getUTCMinutes())}:${pad(time.getUTCSeconds())} UTC`
  );
}

export function formatDate(value: string | null | undefined): string | null {
  const full = formatDateTime(value);
  return full ? full.slice(0, 10) : null;
}

export function formatBytes(value: number | null | undefined): string | null {
  if (value === null || value === undefined || !Number.isFinite(value) || value < 0) return null;
  if (value < 1024) return `${value} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let amount = value / 1024;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit += 1;
  }
  return `${amount.toFixed(amount >= 10 ? 0 : 1)} ${units[unit]}`;
}

export function formatDuration(seconds: number | null | undefined): string | null {
  if (seconds === null || seconds === undefined || !Number.isFinite(seconds) || seconds < 0) return null;
  if (seconds < 60) return `${Math.round(seconds)} 秒`;
  const minutes = Math.floor(seconds / 60);
  const rest = Math.round(seconds % 60);
  if (minutes < 60) return rest === 0 ? `${minutes} 分钟` : `${minutes} 分 ${rest} 秒`;
  const hours = Math.floor(minutes / 60);
  return `${hours} 小时 ${minutes % 60} 分`;
}

export function formatCount(value: number | null | undefined): string {
  return value === null || value === undefined || !Number.isFinite(value) ? "—" : String(value);
}

export function truncate(value: string, max = 80): string {
  return value.length > max ? `${value.slice(0, max)}…` : value;
}

export function isoNow(): string {
  return new Date().toISOString();
}
