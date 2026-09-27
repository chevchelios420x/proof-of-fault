<script>
  import { onMount, onDestroy } from 'svelte'
  import uPlot from 'uplot'

  export let timeline
  export let series = [] // [{key, label, color}] in route order
  export let hidden = new Set() // keys hidden in the chart
  export let onToggle = (key, show) => {} // legend click
  export let version = 0 // bump to redraw
  export let live = false // pause updates while the mouse is over the chart

  let el, plot, ro, builtFor = ''
  let paused = false
  let zoomed = false // user zoomed in: keep the x range on updates
  $: keys = series.map((s) => s.key)
  $: signature = series.map((s) => s.key + '=' + s.label + '=' + s.color).join('|')

  // Red ticks per series lane at the top of the plot mark packet loss.
  const lossPlugin = {
    hooks: {
      draw: (u) => {
        const ctx = u.ctx
        const lane = 6 * devicePixelRatio
        ctx.save()
        ctx.fillStyle = '#d62828'
        let lanePos = 0
        for (let si = 1; si < u.series.length; si++) {
          if (!u.series[si].show) continue
          const ys = u.data[si]
          const top = u.bbox.top + lanePos++ * (lane + 2)
          for (let i = 0; i < ys.length; i++) {
            if (ys[i] !== null) continue
            const x = u.valToPos(u.data[0][i], 'x', true)
            if (x < u.bbox.left || x > u.bbox.left + u.bbox.width) continue
            ctx.fillRect(x - 1, top, Math.max(2, devicePixelRatio * 2), lane)
          }
        }
        ctx.restore()
      },
    },
  }

  function build() {
    plot?.destroy()
    builtFor = signature
    const opts = {
      width: el.clientWidth,
      height: 320,
      plugins: [lossPlugin],
      scales: { x: { time: true }, y: { range: (u, min, max) => [0, Math.max(10, (max || 0) * 1.1)] } },
      axes: [{}, { label: 'RTT (ms)' }],
      series: [
        { value: (u, v) => (v == null ? '–' : new Date(v * 1000).toLocaleTimeString('de-DE')) },
        ...series.map((s) => ({
          label: s.label,
          stroke: s.color,
          width: 1.4,
          show: !hidden.has(s.key),
          spanGaps: false,
          value: (u, v, si, idx) => (idx == null || v === undefined ? '–' : v === null ? 'Verlust' : v.toFixed(1) + ' ms'),
        })),
      ],
      cursor: { drag: { x: true, y: false } },
      hooks: {
        setSeries: [(u, si) => {
          if (si == null) return
          const key = keys[si - 1]
          const show = u.series[si].show
          if (show === hidden.has(key)) onToggle(key, show)
        }],
        setSelect: [(u) => { if (u.select.width > 0) zoomed = true }],
      },
    }
    plot = new uPlot(opts, timeline.data(keys), el)
  }

  function refresh() {
    if (plot && !(live && paused)) plot.setData(timeline.data(keys), !zoomed)
  }

  // Apply visibility changes made outside the chart (route table checkboxes).
  function syncHidden(h) {
    if (!plot) return
    keys.forEach((k, i) => {
      const show = !h.has(k)
      if (plot.series[i + 1].show !== show) plot.setSeries(i + 1, { show })
    })
  }

  $: if (el && series.length && builtFor !== signature) build()
  $: if (plot && version >= 0) refresh()
  $: syncHidden(hidden)

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
  Ziehen = Zoom (bleibt bei neuen Daten erhalten), Doppelklick = zurücksetzen. Rote Markierungen oben = Paketverlust
  (je sichtbarer Linie eine Spur, Reihenfolge wie in der Legende). Linien per Legende oder Route-Tabelle ein-/ausblenden.
</p>

<style>
  .chart { width: 100%; min-height: 320px; }
  .hint { color: var(--muted); font-size: 12px; margin: 4px 0 0; }
  .paused { color: var(--accent); margin-right: 6px; }
</style>
