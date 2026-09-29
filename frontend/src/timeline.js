// Timeline aligns samples of all series (zones and watched hops) on a
// 1-second grid for uPlot.
// y values: number = RTT in ms, null = loss, undefined = no probe.
export class Timeline {
  constructor() { this.reset() }

  reset() {
    this.xs = []
    this.ys = {}
    this.index = new Map()
  }

  add(key, tMs, rttMs) {
    const sec = Math.floor(tMs / 1000)
    let i = this.index.get(sec)
    if (i === undefined) {
      if (this.xs.length && sec < this.xs[this.xs.length - 1]) return // late, out of order: skip
      i = this.xs.length
      this.xs.push(sec)
      for (const k in this.ys) this.ys[k].push(undefined)
      this.index.set(sec, i)
    }
    if (!this.ys[key]) this.ys[key] = new Array(this.xs.length).fill(undefined)
    this.ys[key][i] = rttMs < 0 ? null : rttMs
  }

  loadSeries(series) {
    this.reset()
    const all = []
    for (const s of series) for (let i = 0; i < s.t.length; i++) all.push([s.t[i], s.zone, s.rtt[i]])
    all.sort((a, b) => a[0] - b[0])
    for (const [t, z, r] of all) this.add(z, t, r)
  }

  data(keys) {
    return [this.xs, ...keys.map((k) => this.ys[k] || new Array(this.xs.length).fill(undefined))]
  }
}

// MAX_POINTS limits what the chart has to draw per series. Longer ranges are
// thinned out: each bucket keeps its highest RTT, a loss in the bucket wins
// (so spikes and losses stay visible).
export const MAX_POINTS = 3000

// slice returns uPlot data for keys limited to [fromSec, toSec] (null = open)
// and thinned to at most maxPoints x values.
export function slice(tl, keys, fromSec, toSec, maxPoints = MAX_POINTS) {
  const xs = tl.xs
  const lo = fromSec == null ? 0 : lowerBound(xs, fromSec)
  const hi = toSec == null ? xs.length : lowerBound(xs, toSec + 1)
  const n = Math.max(0, hi - lo)
  const cols = keys.map((k) => tl.ys[k])
  if (n <= maxPoints) {
    return [xs.slice(lo, hi), ...cols.map((c) => (c ? c.slice(lo, hi) : new Array(n).fill(undefined)))]
  }
  const step = n / maxPoints
  const outX = new Array(maxPoints)
  const outY = cols.map(() => new Array(maxPoints))
  for (let b = 0; b < maxPoints; b++) {
    const s = lo + Math.floor(b * step)
    const e = lo + Math.floor((b + 1) * step)
    outX[b] = xs[s]
    cols.forEach((c, ci) => {
      let v // undefined = not probed
      if (c) for (let i = s; i < e; i++) {
        const y = c[i]
        if (y === null) { v = null; break }
        if (y !== undefined && (v === undefined || y > v)) v = y
      }
      outY[ci][b] = v
    })
  }
  return [outX, ...outY]
}

function lowerBound(a, x) {
  let l = 0, h = a.length
  while (l < h) { const m = (l + h) >> 1; if (a[m] < x) l = m + 1; else h = m }
  return l
}
