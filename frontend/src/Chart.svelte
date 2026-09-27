<script>
  import { onMount, onDestroy } from 'svelte'
  import uPlot from 'uplot'
  import { seriesColor, seriesLabel } from './zones.js'

  export let timeline
  export let zones = [] // series keys: zones and hop keys
  export let hops = [] // for hop labels
  export let version = 0 // bump to redraw
  export let live = false // pause updates while the mouse is over the chart

  let el, plot, ro, builtFor = ''
  let paused = false // mouse over the chart (live mode)
  let zoomed = false // user zoomed in: keep the x range on updates
  const hidden = new Set() // series keys hidden via the legend, kept across rebuilds

  // Red ticks per zone lane at the top of the plot mark packet loss.
  const lossPlugin = {
    hooks: {
      draw: (u) => {
        const ctx = u.ctx
        const lane = 6 * devicePixelRatio
        ctx.save()
        zones.forEach((z, zi) => {
          if (!u.series[zi + 1].show) return
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
          show: !hidden.has(z),
          stroke: seriesColor(z),
          width: 1.2,
          spanGaps: false,
          value: (u, v, si, idx) => (idx == null || v === undefined ? '–' : v === null ? 'Verlust' : v.toFixed(1) + ' ms'),
        })),
      ],
      cursor: { drag: { x: true, y: false } },
      hooks: {
        setSeries: [(u, si) => {
          if (si == null) return
          const key = zones[si - 1]
          u.series[si].show ? hidden.delete(key) : hidden.add(key)
        }],
        setSelect: [(u) => { if (u.select.width > 0) zoomed = true }],
      },
    }
    plot = new uPlot(opts, timeline.data(zones), el)
  }

  $: if (el && zones.length && builtFor !== zones.join(',') + '|' + zones.map((z) => seriesLabel(z, hops)).join(',')) build()
  function refresh() {
    if (plot && !(live && paused)) plot.setData(timeline.data(zones), !zoomed)
  }
  $: if (plot && version >= 0) refresh()

  function enter() { paused = true }
  function leave() { paused = false; refresh() }
  function resetZoom() { zoomed = false; refresh() }

  onMount(() => {
    ro = new ResizeObserver(() => plot?.setSize({ width: el.clientWidth, height: 320 }))
    ro.observe(el)
  })
  onDestroy(() => { ro?.disconnect(); plot?.destroy() })
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div bind:this={el} class="chart" on:mouseenter={enter} on:mouseleave={leave} on:dblclick={resetZoom}></div>
<p class="hint">
  {#if live && paused}<b class="paused">⏸ Angehalten, solange die Maus über dem Diagramm ist.</b>{/if}
  Ziehen = Zoom (bleibt bei neuen Daten erhalten), Doppelklick = zurücksetzen. Rote Markierungen oben = Paketverlust (je Linie eine Spur, gleiche Reihenfolge wie die Legende). Linien lassen sich per Klick auf die Legende ausblenden.</p>

<style>
  .chart { width: 100%; min-height: 320px; }
  .hint { color: var(--muted); font-size: 12px; margin: 4px 0 0; }
  .paused { color: var(--accent); margin-right: 6px; }
</style>
