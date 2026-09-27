<script>
  import { onMount, onDestroy } from 'svelte'
  import uPlot from 'uplot'
  import { seriesColor, seriesLabel } from './zones.js'

  export let timeline
  export let zones = [] // series keys: zones and hop keys
  export let hops = [] // for hop labels
  export let version = 0 // bump to redraw

  let el, plot, ro, builtFor = ''

  // Red ticks per zone lane at the top of the plot mark packet loss.
  const lossPlugin = {
    hooks: {
      draw: (u) => {
        const ctx = u.ctx
        const lane = 6 * devicePixelRatio
        ctx.save()
        zones.forEach((z, zi) => {
          const ys = u.data[zi + 1]
          const top = u.bbox.top + zi * (lane + 2)
          for (let i = 0; i < ys.length; i++) {
            if (ys[i] !== null) continue
            const x = u.valToPos(u.data[0][i], 'x', true)
            if (x < u.bbox.left || x > u.bbox.left + u.bbox.width) continue
            ctx.fillStyle = '#d62828'
            ctx.fillRect(x - 1, top, Math.max(2, devicePixelRatio * 2), lane)
          }
        })
        ctx.restore()
      },
    },
  }

  function build() {
    plot?.destroy()
    builtFor = zones.join(',') + '|' + zones.map((z) => seriesLabel(z, hops)).join(',')
    const opts = {
      width: el.clientWidth,
      height: 320,
      plugins: [lossPlugin],
      scales: { x: { time: true }, y: { range: (u, min, max) => [0, Math.max(10, max * 1.1)] } },
      axes: [{}, { label: 'RTT (ms)' }],
      series: [
        { value: (u, v) => (v == null ? '–' : new Date(v * 1000).toLocaleTimeString('de-DE')) },
        ...zones.map((z) => ({
          label: seriesLabel(z, hops),
          stroke: seriesColor(z),
          width: 1.2,
          spanGaps: false,
          value: (u, v) => (v == null ? 'Verlust' : v.toFixed(1) + ' ms'),
        })),
      ],
      cursor: { drag: { x: true, y: false } },
    }
    plot = new uPlot(opts, timeline.data(zones), el)
  }

  $: if (el && zones.length && builtFor !== zones.join(',') + '|' + zones.map((z) => seriesLabel(z, hops)).join(',')) build()
  $: if (plot && version >= 0) plot.setData(timeline.data(zones), true)

  onMount(() => {
    ro = new ResizeObserver(() => plot?.setSize({ width: el.clientWidth, height: 320 }))
    ro.observe(el)
  })
  onDestroy(() => { ro?.disconnect(); plot?.destroy() })
</script>

<div bind:this={el} class="chart"></div>
<p class="hint">Ziehen = Zoom, Doppelklick = zurücksetzen. Rote Markierungen oben = Paketverlust (je Linie eine Spur, gleiche Reihenfolge wie die Legende). Linien lassen sich per Klick auf die Legende ausblenden.</p>

<style>
  .chart { width: 100%; min-height: 320px; }
  .hint { color: var(--muted); font-size: 12px; margin: 4px 0 0; }
</style>
