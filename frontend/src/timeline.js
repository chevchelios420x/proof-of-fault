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
