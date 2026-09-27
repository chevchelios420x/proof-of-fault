import { ZONES } from './zones.js'

// Timeline aligns samples of all zones on a 1-second grid for uPlot.
// y values: number = RTT in ms, null = loss, undefined = no probe.
export class Timeline {
  constructor() { this.reset() }

  reset() {
    this.xs = []
    this.ys = Object.fromEntries(ZONES.map((z) => [z, []]))
    this.index = new Map()
  }

  add(zone, tMs, rttMs) {
    const sec = Math.floor(tMs / 1000)
    let i = this.index.get(sec)
    if (i === undefined) {
      if (this.xs.length && sec < this.xs[this.xs.length - 1]) return // late, out of order: skip
      i = this.xs.length
      this.xs.push(sec)
      for (const z of ZONES) this.ys[z].push(undefined)
      this.index.set(sec, i)
    }
    this.ys[zone][i] = rttMs < 0 ? null : rttMs
  }

  loadSeries(series) {
    this.reset()
    const all = []
    for (const s of series) for (let i = 0; i < s.t.length; i++) all.push([s.t[i], s.zone, s.rtt[i]])
    all.sort((a, b) => a[0] - b[0])
    for (const [t, z, r] of all) this.add(z, t, r)
  }

  data(zones) {
    return [this.xs, ...zones.map((z) => this.ys[z])]
  }
}
