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

const HOP_PALETTE = ['#8e44ad', '#d4a017', '#16a085', '#c0392b', '#2980b9', '#7f8c8d', '#e67e22', '#27ae60', '#6c5ce7', '#b33771']

export const hopKey = (addr) => 'hop:' + addr
export const isHopKey = (k) => k.startsWith('hop:')

// seriesColor gives zones their fixed color and hops a stable palette color.
export function seriesColor(key) {
  if (ZONE_COLOR[key]) return ZONE_COLOR[key]
  let h = 0
  for (const c of key) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return HOP_PALETTE[h % HOP_PALETTE.length]
}

// seriesLabel names a series; hops as "Hop <ttl> <addr>" when the TTL is known.
export function seriesLabel(key, hops = []) {
  if (!isHopKey(key)) return key
  const addr = key.slice(4)
  const h = hops.find((x) => x.addr === addr)
  return h ? `Hop ${h.ttl} ${addr}` : addr
}
