// Acoustic signals for events (Web Audio, no sound files needed).
let ctx = null
function ac() {
  if (!ctx) ctx = new (window.AudioContext || window.webkitAudioContext)()
  if (ctx.state === 'suspended') ctx.resume()
  return ctx
}

// Tone patterns: [frequency Hz, duration s, pause s] …
const PATTERNS = {
  route: [[660, 0.12, 0.05], [880, 0.18, 0]],               // rising two-tone
  spike: [[1200, 0.08, 0]],                                   // short high blip
  loss: [[520, 0.12, 0.06], [520, 0.12, 0]],                  // double beep
  outage: [[440, 0.25, 0.08], [330, 0.25, 0.08], [440, 0.35, 0]], // alarm
}

export function play(kind, volume = 60) {
  const p = PATTERNS[kind]
  if (!p) return
  const a = ac()
  let t = a.currentTime + 0.02
  const gainMax = Math.max(0, Math.min(1, volume / 100)) * 0.4
  for (const [f, d, pause] of p) {
    const o = a.createOscillator()
    const g = a.createGain()
    o.type = 'sine'
    o.frequency.value = f
    g.gain.setValueAtTime(0, t)
    g.gain.linearRampToValueAtTime(gainMax, t + 0.01)
    g.gain.setValueAtTime(gainMax, t + d - 0.02)
    g.gain.linearRampToValueAtTime(0, t + d)
    o.connect(g).connect(a.destination)
    o.start(t)
    o.stop(t + d + 0.01)
    t += d + pause
  }
}

const last = {}

// signalFor plays the sound configured for a log event, if any.
export function signalFor(e, sounds) {
  if (!sounds?.enabled || !e) return
  let kind = null
  if (e.kind === 'path_change') kind = sounds.routeChange ? 'route' : null
  else {
    const z = sounds.zones?.[e.zone]
    if (!z) return
    if (e.kind === 'spike' && z.spike) kind = 'spike'
    else if ((e.kind === 'loss' || e.kind === 'loss_device') && z.loss) kind = 'loss'
    else if (e.kind === 'outage_start' && z.outage) kind = 'outage'
  }
  if (!kind) return
  const key = kind + '|' + (e.zone || '')
  const now = Date.now()
  if (last[key] && now - last[key] < (sounds.cooldownSec || 0) * 1000) return
  last[key] = now
  play(kind, sounds.volume)
}
