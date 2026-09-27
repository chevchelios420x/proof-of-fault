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
