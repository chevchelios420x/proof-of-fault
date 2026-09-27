export const ZONES = ['LAN', 'ISP_EDGE', 'WAN']
export const ZONE_COLOR = { LAN: '#2a9d8f', ISP_EDGE: '#e76f51', WAN: '#3a5a8c' }
export const ZONE_LABEL = {
  LAN: 'LAN – Heimnetz / Router',
  ISP_EDGE: 'ISP_EDGE – Provider-Zugang',
  WAN: 'WAN – Internet / Ziel',
}
export const fmtTime = (ms) => (ms ? new Date(ms).toLocaleString('de-DE') : 'läuft')
export const fmtDur = (s) => {
  s = Math.round(s || 0)
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60
  return (h ? h + 'h ' : '') + (h || m ? m + 'm ' : '') + sec + 's'
}

// One clearly distinguishable color per chart line. Red is left out (it
// marks losses); zones are shown by grouping, not by color.
const PALETTE = ['#1f77b4', '#ff7f0e', '#2ca02c', '#9467bd', '#17becf', '#8c564b', '#e377c2', '#222222', '#bcbd22', '#000080', '#7f7f7f', '#00a087', '#b8860b', '#6a5acd']

export const hopKey = (addr) => 'hop:' + addr
export const isHopKey = (k) => k.startsWith('hop:')
export const devKey = (host) => 'dev:' + host

export const GROUPS = ['LAN', 'ISP_EDGE', 'WAN', 'none']
export const GROUP_LABEL = { LAN: 'LAN – Heimnetz', ISP_EDGE: 'ISP_EDGE – Anbieter', WAN: 'WAN – Internet/Ziele', none: 'Nicht gewertet' }

// colorMap assigns every route row and manual point a unique color, in
// route order, so table dots and chart lines always match.
export function colorMap(hops = [], reps = [], custom = []) {
  const repZone = Object.fromEntries(reps.map((r) => [r.ip, r.zone]))
  const keys = []
  for (const h of hops) {
    if (!h.responsive) continue
    const k = repZone[h.addr] || hopKey(h.addr)
    if (!keys.includes(k)) keys.push(k)
  }
  for (const r of reps) if (!keys.includes(r.zone)) keys.push(r.zone)
  for (const c of custom) keys.push(devKey(c.host))
  return Object.fromEntries(keys.map((k, i) => [k, PALETTE[i % PALETTE.length]]))
}

// hopLabel: "Hop 2 · Heimrouter" (name) or "Hop 2 · 192.168.0.1".
export function hopLabel(ttl, addr, name, repZone) {
  const base = ttl ? `Hop ${ttl} · ${name || addr}` : name || addr
  return repZone ? `${base} (Messpunkt)` : base
}

// buildSeries returns the chart series in route order (TTL), followed by
// manual points. Each series carries its zone group for the legend.
export function buildSeries({ hops = [], reps = [], watched = [], names = {}, keys = null, custom = [] }) {
  const repZone = Object.fromEntries(reps.map((r) => [r.ip, r.zone]))
  const colors = colorMap(hops, reps, custom)
  const out = []
  const seen = new Set()
  for (const h of hops) {
    if (!h.responsive || seen.has(h.addr)) continue
    seen.add(h.addr)
    const rz = repZone[h.addr]
    const key = rz || hopKey(h.addr)
    if (keys ? !keys.includes(key) : !rz && !watched.includes(h.addr)) continue
    out.push({ key, addr: h.addr, ttl: h.ttl, group: rz || h.zone, label: hopLabel(h.ttl, h.addr, names[h.addr], rz), color: colors[key] })
  }
  for (const r of reps) {
    if (seen.has(r.ip) || (keys && !keys.includes(r.zone))) continue
    out.push({ key: r.zone, addr: r.ip, ttl: 0, group: r.zone, label: hopLabel(0, r.ip, names[r.ip], r.zone), color: colors[r.zone] })
  }
  custom.forEach((c) => {
    const k = devKey(c.host)
    if (!c.enabled || !c.ip || (keys && !keys.includes(k))) return
    out.push({ key: k, addr: c.ip, ttl: 0, group: c.zone || 'none', label: `${c.name || c.host}`, color: colors[k], dash: true })
  })
  for (const k of keys || []) {
    if (out.some((x) => x.key === k)) continue
    const isDev = k.startsWith('dev:')
    const addr = isHopKey(k) || isDev ? k.slice(4) : k
    out.push({ key: k, addr, ttl: 0, group: isHopKey(k) || isDev ? 'none' : k, label: names[addr] || addr, color: PALETTE[out.length % PALETTE.length], dash: isDev })
  }
  return out
}
