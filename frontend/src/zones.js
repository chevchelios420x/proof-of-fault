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

// Distinct colors for individual hops; zone colors and greys are excluded so
// every line is clearly tied to one row of the route table.
const HOP_PALETTE = ['#8e44ad', '#d4a017', '#1f77b4', '#c2185b', '#17becf', '#8c564b', '#bcbd22', '#ff7f0e', '#6a3d9a', '#b15928', '#e377c2', '#2ca02c']

export const hopKey = (addr) => 'hop:' + addr
export const isHopKey = (k) => k.startsWith('hop:')

// hopColor: zone representatives use their zone color, other hops a palette
// color by TTL (stable and unique along the path).
export function hopColor(ttl, repZone) {
  if (repZone) return ZONE_COLOR[repZone]
  return HOP_PALETTE[(ttl - 1) % HOP_PALETTE.length]
}

// hopLabel: "Hop 2 · Heimrouter" (name) or "Hop 2 · 192.168.0.1", plus the
// zone for zone measuring points.
export function hopLabel(ttl, addr, name, repZone) {
  const base = ttl ? `Hop ${ttl} · ${name || addr}` : name || addr
  return repZone ? `${base} (${repZone})` : base
}

// buildSeries returns the chart series in route order (TTL). Zone
// measuring points keep their zone key; other hops appear when watched.
export function buildSeries({ hops = [], reps = [], watched = [], names = {}, keys = null }) {
  const repZone = Object.fromEntries(reps.map((r) => [r.ip, r.zone]))
  const out = []
  const seen = new Set()
  for (const h of hops) {
    if (!h.responsive || seen.has(h.addr)) continue
    seen.add(h.addr)
    const rz = repZone[h.addr]
    const key = rz || hopKey(h.addr)
    if (keys ? !keys.includes(key) : !rz && !watched.includes(h.addr)) continue
    out.push({ key, addr: h.addr, ttl: h.ttl, zone: rz, label: hopLabel(h.ttl, h.addr, names[h.addr], rz), color: hopColor(h.ttl, rz) })
  }
  // Series without a hop in the table (e.g. target that filters traceroute).
  for (const r of reps) {
    if (seen.has(r.ip) || (keys && !keys.includes(r.zone))) continue
    out.push({ key: r.zone, addr: r.ip, ttl: 0, zone: r.zone, label: hopLabel(0, r.ip, names[r.ip], r.zone), color: ZONE_COLOR[r.zone] })
  }
  for (const k of keys || []) {
    if (out.some((x) => x.key === k)) continue
    const addr = isHopKey(k) ? k.slice(4) : k
    out.push({ key: k, addr, ttl: 0, zone: isHopKey(k) ? '' : k, label: names[addr] || addr, color: ZONE_COLOR[k] || '#555' })
  }
  return out
}
