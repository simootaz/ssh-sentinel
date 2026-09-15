// Formatting helpers: dates, durations, sizes. Pure functions, no DOM, so
// they can be unit-tested with node --test (web/test).

const pad = (n) => String(n).padStart(2, '0');

/** RFC 3339 string to "YYYY-MM-DD HH:MM:SS" in the browser's time zone. */
export function fmtTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return String(iso);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/** Seconds to a short duration: "45 s", "3 min", "2 h", "5 d". */
export function fmtDuration(seconds) {
  const s = Math.max(0, Math.round(seconds));
  if (s < 60) return `${s} s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min`;
  const h = s / 3600;
  if (h < 48) return Number.isInteger(h) ? `${h} h` : `${h.toFixed(1)} h`;
  const d = s / 86400;
  return Number.isInteger(d) ? `${d} d` : `${d.toFixed(1)} d`;
}

/** "3 min ago" for a past instant, "in 3 min" for a future one. */
export function fmtRelative(iso, now = Date.now()) {
  if (!iso) return '';
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return String(iso);
  const diff = (t - now) / 1000;
  if (Math.abs(diff) < 1) return 'now';
  return diff < 0 ? `${fmtDuration(-diff)} ago` : `in ${fmtDuration(diff)}`;
}

/** Whole seconds until iso, never negative. */
export function secondsLeft(iso, now = Date.now()) {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return 0;
  return Math.max(0, Math.ceil((t - now) / 1000));
}

/** Expiry of a whitelist entry or a block: null means permanent. */
export function fmtExpiry(iso, now = Date.now()) {
  if (!iso) return 'permanent';
  return `${fmtTime(iso)} (${fmtRelative(iso, now)})`;
}

/** Bytes to "48.2 KB". */
export function fmtBytes(n) {
  if (n == null) return '';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * TTL choice of the "always allow" and whitelist forms to ttl_seconds.
 * choice: "permanent" | "1h" | "24h" | "custom"; for custom, amount and unit
 * ("min" | "h" | "d"). Returns null for permanent, a positive integer of
 * seconds otherwise, and throws on an invalid custom value.
 */
export function ttlSeconds(choice, amount, unit) {
  switch (choice) {
    case 'permanent': return null;
    case '1h': return 3600;
    case '24h': return 86400;
    case 'custom': {
      const n = Number(amount);
      if (!Number.isFinite(n) || n <= 0) throw new Error('duration must be a positive number');
      const mult = { min: 60, h: 3600, d: 86400 }[unit];
      if (!mult) throw new Error('unknown unit');
      return Math.round(n * mult);
    }
    default: throw new Error('unknown TTL choice');
  }
}

/** Geo object of the contract to "Paris, FR (AS12876)"; empty when unknown. */
export function fmtGeo(geo) {
  if (!geo) return '';
  const parts = [];
  if (geo.city) parts.push(geo.city);
  if (geo.country) parts.push(geo.country);
  let s = parts.join(', ');
  if (geo.asn) s += s ? ` (${geo.asn})` : geo.asn;
  return s;
}

/**
 * Who decided a request, from the v1 and v1.1 fields: the admin name, the
 * phone label, or the rule. Any of them may be missing on an older backend.
 */
export function fmtDecider(item) {
  const who = [];
  if (item.decided_by_admin) who.push(item.decided_by_admin);
  if (item.decided_by_device) who.push(item.decided_by_device);
  if (who.length) return who.join(' / ');
  switch (item.decided_by) {
    case 'admin': return 'admin';
    case 'whitelist': return 'whitelist';
    case 'timeout': return 'nobody (timeout)';
    case 'autoblock': return 'auto-block';
    case 'georule': return 'geo rule';
    default: return '';
  }
}
