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
const PALETTE = ['#1f77b4', '#ff7f0e', '#2ca02c', '#9467bd', '#17becf', '#8c564b', '#e377c2', '#bcbd22', '#7f7f7f', '#00a087', '#daa520', '#6a5acd', '#ff69b4', '#4682b4']

export const hopKey = (addr) => 'hop:' + addr
export const isHopKey = (k) => k.startsWith('hop:')
export const devKey = (host) => 'dev:' + host

// Default zone list (the real one comes from the settings).
export const DEFAULT_ZONES = [
  { id: 'LAN', name: 'LAN – Heimnetz', color: '#2a9d8f', role: 'LAN', builtin: true },
  { id: 'ISP_EDGE', name: 'ISP_EDGE – Anbieter', color: '#e76f51', role: 'ISP_EDGE', builtin: true },
  { id: 'WAN', name: 'WAN – Internet/Ziele', color: '#3a5a8c', role: 'WAN', builtin: true },
  { id: 'none', name: 'Nicht gewertet', color: '#888888', role: 'none', builtin: true },
]

// groupBy sorts items into the configured zones (in their order); items of
// unknown zones end up in "Sonstige".
export function groupBy(items, zoneOf, zones = DEFAULT_ZONES) {
  const out = []
  const used = new Set()
  for (const z of zones) {
    const list = items.filter((it) => zoneOf(it) === z.id)
    list.forEach((it) => used.add(it))
    if (list.length) out.push({ zone: z, items: list })
  }
  const rest = items.filter((it) => !used.has(it))
  if (rest.length) out.push({ zone: { id: '?', name: 'Sonstige', color: '#888888' }, items: rest })
  return out
}

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
  for (const c of custom) {
    keys.push(devKey(c.host))
    keys.push('tcp:' + c.host) // always, so colors stay stable when toggling
  }
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
    if (!c.ip) return
    if (c.enabled && (!keys || keys.includes(k))) out.push({ key: k, addr: c.ip, ttl: 0, group: c.zone || 'none', label: `${c.name || c.host}`, color: colors[k], dash: true })
    const tk = 'tcp:' + c.host
    if (c.tcp && (!keys || keys.includes(tk))) {
      out.push({ key: tk, addr: c.ip, ttl: 0, group: c.zone || 'none', label: `${c.name || c.host} · TCP:${c.port || 443}`, color: colors[tk], dash: true, dot: true })
    }
  })
  for (const k of keys || []) {
    if (out.some((x) => x.key === k)) continue
    const isDev = k.startsWith('dev:') || k.startsWith('tcp:')
    const addr = isHopKey(k) || isDev ? k.slice(4) : k
    out.push({ key: k, addr, ttl: 0, group: isHopKey(k) || isDev ? 'none' : k, label: names[addr] || addr, color: PALETTE[out.length % PALETTE.length], dash: isDev })
  }
  return out
}
