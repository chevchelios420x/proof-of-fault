<script>
  import { onMount, onDestroy } from 'svelte'
  import uPlot from 'uplot'
  import Help from './Help.svelte'
  import { DEFAULT_ZONES, groupBy } from './zones.js'
  import { slice } from './timeline.js'

  export let timeline
  export let series = [] // [{key, label, color, group, dash}] in route order
  export let hidden = new Set() // keys hidden in the chart
  export let onToggle = (key, show) => {} // legend click
  export let version = 0 // bump to redraw
  export let live = false // pause updates while the mouse is over the chart
  export let bands = [] // incidents [{t, end, class}] shaded in the plot
  export let zones = DEFAULT_ZONES // configured zones (legend groups)
  export let theme = 0 // bump when the color scheme changes (axis colors)
  export let windowMin = 30 // live view: visible minutes (0 = whole session)

  let el, plot, ro, builtFor = ''
  let paused = false
  let zoomed = false // user zoomed in: keep the x range on updates
  let scaleMode = 'auto' // auto | linear | log

  let logActive = false
  let cursorT = null
  let cursorVals = {}

  $: keys = series.map((s) => s.key)
  $: signature = series.map((s) => s.key + '=' + s.label + '=' + s.color).join('|') + '#' + theme
  const css = (n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim()
  $: groups = groupBy(series, (s) => s.group || 'none', zones)

  // Auto: logarithmic once the data spans roughly 1 ms … 1000 ms, so
  // single spikes do not flatten all normal values.
  function wantLog(d) {
    if (scaleMode !== 'auto') return scaleMode === 'log'
    let min = Infinity, max = 0
    for (let c = 1; c < d.length; c++) for (const v of d[c]) if (v > 0) { if (v < min) min = v; if (v > max) max = v }
    if (logActive) return max >= 50 // hysteresis: stay logarithmic
    return max >= 100 && max / min >= 50
  }

  const BAND = { target: 'rgba(58,90,140,0.15)', alt: 'rgba(58,90,140,0.15)', lan: 'rgba(42,157,143,0.18)', device: 'rgba(42,157,143,0.18)' }
  const plugin = {
    hooks: {
      drawAxes: (u) => {
        const ctx = u.ctx
        ctx.save()
        for (const b of bands) {
          const x0 = Math.max(u.valToPos(b.t / 1000, 'x', true), u.bbox.left)
          const x1 = Math.min(u.valToPos(b.end / 1000, 'x', true), u.bbox.left + u.bbox.width)
          if (x1 < x0) continue
          ctx.fillStyle = BAND[b.class] || 'rgba(214,40,40,0.18)'
          ctx.fillRect(x0, u.bbox.top, Math.max(2, x1 - x0), u.bbox.height)
        }
        ctx.restore()
      },
      // Red ticks per visible line at the top mark packet loss.
      draw: (u) => {
        const ctx = u.ctx
        const lane = 5 * devicePixelRatio
        ctx.save()
        let pos = 0
        for (let si = 1; si < u.series.length; si++) {
          if (!u.series[si].show) continue
          const ys = u.data[si]
          const top = u.bbox.top + pos++ * (lane + 1)
          ctx.fillStyle = '#d62828'
          for (let i = 0; i < ys.length; i++) {
            if (ys[i] !== null) continue
            const x = u.valToPos(u.data[0][i], 'x', true)
            if (x < u.bbox.left || x > u.bbox.left + u.bbox.width) continue
            ctx.fillRect(x - 1, top, Math.max(2, devicePixelRatio * 2), lane)
          }
          ctx.fillStyle = u.series[si]._stroke || '#999'
          ctx.fillRect(u.bbox.left - 4 * devicePixelRatio, top, 3 * devicePixelRatio, lane)
        }
        ctx.restore()
      },
      setCursor: (u) => {
        const i = u.cursor.idx
        cursorT = i == null ? null : u.data[0][i]
        const v = {}
        if (i != null) keys.forEach((k, si) => (v[k] = u.data[si + 1][i]))
        cursorVals = v
      },
    },
  }

  function build() {
    plot?.destroy()
    const d = rawData()
    logActive = wantLog(d)
    builtFor = signature
    const opts = {
      width: el.clientWidth,
      height: 340,
      plugins: [plugin],
      legend: { show: false },
      scales: {
        x: { time: true },
        y: logActive
          ? { distr: 3, log: 10, range: (u, min, max) => uPlot.rangeLog(Math.max(0.5, min || 1), Math.max(10, max || 10), 10, true) }
          : { range: (u, min, max) => [0, Math.max(10, (max || 0) * 1.1)] },
      },
      axes: [
        { stroke: css('--axis'), grid: { stroke: css('--grid') }, ticks: { stroke: css('--grid') } },
        { label: logActive ? 'RTT (ms, logarithmisch)' : 'RTT (ms)', size: 60, stroke: css('--axis'), grid: { stroke: css('--grid') }, ticks: { stroke: css('--grid') } },
      ],
      series: [
        {},
        ...series.map((s) => ({
          label: s.label,
          // Function: evaluated on every redraw, so highlighting a group
          // needs no rebuild.
          stroke: () => (dimmed(s) ? fade(s.color) : s.color),
          width: s.dash ? 1.4 : 1.6,
          dash: s.dot ? [2, 3] : s.dash ? [6, 3] : undefined,
          show: !hidden.has(s.key),
          spanGaps: false,
        })),
      ],
      cursor: { drag: { x: true, y: false } },
      hooks: {
        // After a drag zoom reload the range in full resolution.
        setSelect: [(u) => { if (u.select.width > 0) { zoomed = true; setTimeout(reload) } }],
      },
    }
    plot = new uPlot(opts, lift(d), el)
  }

  // rawData returns only what is visible: the zoomed range, the live window
  // or the whole session – thinned out for long ranges. Handing uPlot the
  // complete session every second made the GUI stutter or hang.
  function rawData() {
    if (zoomed && plot) {
      const { min, max } = plot.scales.x
      if (min != null && max != null) return slice(timeline, keys, Math.floor(min), Math.ceil(max))
    }
    const xs = timeline.xs
    if (live && windowMin > 0 && xs.length) return slice(timeline, keys, xs[xs.length - 1] - windowMin * 60, null)
    return slice(timeline, keys, null, null)
  }

  // Log scale cannot show 0 ms; tiny values are lifted to 0.1 ms.
  function lift(d) {
    if (!logActive) return d
    return d.map((arr, i) => (i === 0 ? arr : arr.map((v) => (v != null && v <= 0.1 ? 0.1 : v))))
  }

  function refresh() {
    if (!plot || (live && paused)) return
    const d = rawData()
    if (wantLog(d) !== logActive) return build()
    plot.setData(lift(d), !zoomed)
  }

  function reload() {
    if (plot) plot.setData(lift(rawData()), !zoomed)
  }

  let lastWindow = windowMin
  $: if (windowMin !== lastWindow) { lastWindow = windowMin; zoomed = false; reload() }

  function toggle(k) {
    const show = hidden.has(k)
    onToggle(k, show)
  }

  // Apply visibility changes (legend or route table checkboxes).
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
  let lastScale = scaleMode
  $: if (scaleMode !== lastScale) { lastScale = scaleMode; if (plot) build() }
  $: if (plot && bands) plot.redraw(false)

  // Group highlighting: the focused group keeps its colors, all other lines
  // are dimmed. Hovering a group name previews it.
  let focusGroup = null
  let hoverGroup = null
  let hoverKey = null
  export let highlight = null // series key highlighted from outside (table row hover)
  $: activeFocus = hoverGroup || focusGroup
  $: activeKey = hoverKey || highlight
  // A single highlighted line wins over group highlighting.
  function dimmed(s) {
    if (activeKey) return s.key !== activeKey
    return !!activeFocus && (s.group || 'none') !== activeFocus
  }
  $: if (plot && (activeFocus !== undefined || activeKey !== undefined)) plot.redraw(false)
  function fade(hex) {
    const m = /^#?([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex || '')
    if (!m) return 'rgba(128,128,128,0.12)'
    return `rgba(${parseInt(m[1], 16)},${parseInt(m[2], 16)},${parseInt(m[3], 16)},0.12)`
  }

  // Clicking a group name shows or hides the whole group: if any line of
  // it is visible, all are hidden, otherwise all are shown.
  function toggleGroup(grp) {
    const anyVisible = grp.items.some((s) => !hidden.has(s.key))
    for (const s of grp.items) {
      if (hidden.has(s.key) === anyVisible) continue // already in the target state
      onToggle(s.key, !anyVisible)
    }
  }

  function enter() { paused = true }
  function leave() { paused = false; refresh() }
  function resetZoom() { zoomed = false; refresh() }

  // zoomTo shows the given time range (unix ms), e.g. an incident.
  export function zoomTo(fromMs, toMs) {
    if (!plot) return
    zoomed = true
    plot.setData(lift(slice(timeline, keys, Math.floor(fromMs / 1000), Math.ceil(toMs / 1000))), false)
    plot.setScale('x', { min: fromMs / 1000, max: toMs / 1000 })
  }

  const fmtV = (v) => (v === undefined ? '–' : v === null ? 'Verlust' : (logActive && v <= 0.1 ? '<0.1' : v.toFixed(1)) + ' ms')

  onMount(() => {
    ro = new ResizeObserver(() => plot?.setSize({ width: el.clientWidth, height: 340 }))
    ro.observe(el)
  })
  onDestroy(() => { ro?.disconnect(); plot?.destroy() })
</script>

<div class="tools">
  <span class="time">{cursorT ? new Date(cursorT * 1000).toLocaleString('de-DE') : 'Maus über das Diagramm bewegen für Einzelwerte'}</span>
  {#if live}
    <label class="win">Zeitraum
      <select bind:value={windowMin}>
        <option value={5}>5 min</option>
        <option value={30}>30 min</option>
        <option value={60}>60 min</option>
        <option value={90}>90 min</option>
        <option value={0}>gesamte Messung</option>
      </select>
      <Help text={'Wie viel Zeit das Live-Diagramm zeigt – es läuft mit den neuesten Messwerten mit. Ältere Daten gehen nicht verloren: „gesamte Messung“ wählen oder unter „Verlauf & Berichte“ ansehen. Ein Zoom per Ziehen hält die Ansicht fest, Doppelklick kehrt zum gewählten Zeitraum zurück.'} />
    </label>
  {/if}
  <label>Skala
    <select bind:value={scaleMode}>
      <option value="auto">automatisch</option>
      <option value="linear">linear</option>
      <option value="log">logarithmisch</option>
    </select>
    <Help align="right" text={'Automatisch: logarithmisch, sobald Werte von wenigen ms bis mehrere 100 ms vorkommen – dann bleiben normale Werte trotz einzelner Spitzen gut lesbar. Auf einer logarithmischen Skala hat jede Zehnerstufe (1, 10, 100, 1000 ms) den gleichen Abstand.'} />
  </label>
</div>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div bind:this={el} class="chart" on:mouseenter={enter} on:mouseleave={leave} on:dblclick={resetZoom}></div>

<div class="legend">
  {#each groups as grp}
    <div class="group" class:focused={focusGroup === grp.zone.id} class:dimmed={activeFocus && activeFocus !== grp.zone.id}>
      <div class="gname" style="border-bottom-color:{grp.zone.color}">
        <button class="gtoggle" class:alloff={grp.items.every((s) => hidden.has(s.key))}
          title="Klicken: ganze Gruppe im Diagramm ein-/ausblenden"
          on:click={() => toggleGroup(grp)}
          on:mouseenter={() => (hoverGroup = grp.zone.id)} on:mouseleave={() => (hoverGroup = null)}>{grp.zone.name}</button>
        <button class="focus" class:on={focusGroup === grp.zone.id}
          title="Gruppe hervorheben: alle anderen Linien werden abgeblendet (nochmal klicken zum Aufheben)"
          on:click={() => (focusGroup = focusGroup === grp.zone.id ? null : grp.zone.id)}>◉</button>
      </div>
      {#each grp.items as s}
        <button class="item" class:off={hidden.has(s.key)} class:hl={activeKey === s.key} on:click={() => toggle(s.key)} title="Klicken zum Ein-/Ausblenden"
          on:mouseenter={() => (hoverKey = s.key)} on:mouseleave={() => (hoverKey = null)} on:focus={() => (hoverKey = s.key)} on:blur={() => (hoverKey = null)}>
          <svg width="26" height="10"><line x1="1" y1="5" x2="25" y2="5" stroke={s.color} stroke-width="3" stroke-dasharray={s.dot ? '2 3' : s.dash ? '6 3' : ''} /></svg>
          <span class="lbl">{s.label}</span>
          <span class="val" class:lost={cursorVals[s.key] === null}>{cursorT ? fmtV(cursorVals[s.key]) : ''}</span>
        </button>
      {/each}
    </div>
  {/each}
</div>

<p class="hint">
  {#if live && paused}<b class="paused">⏸ Angehalten, solange die Maus über dem Diagramm ist.</b>{/if}
  Klick auf einen Gruppennamen = ganze Gruppe ein-/ausblenden, ◉ = Gruppe hervorheben. Ziehen = Zoom, Doppelklick = zurücksetzen. Farbige Flächen = Störungen (rot: Anbieter/Internet, blau: nur Ziel/Ausweichziel, grün: Heimnetz/Einzelgerät).
  Rote Striche oben = Paketverlust, je sichtbarer Linie eine Spur (Farbmarke links). Gestrichelt = manuelle Messpunkte (Ping), gepunktet = TCP-Check.
</p>

<style>
  .tools { display: flex; justify-content: space-between; align-items: center; font-size: 12px; margin-bottom: 4px; gap: 10px; }
  .tools .time { color: var(--muted); }
  .tools select { padding: 2px 6px; font-size: 12px; }
  .tools .win { margin-left: auto; }
  .chart { width: 100%; min-height: 340px; }
  .legend { display: flex; flex-wrap: wrap; gap: 10px 22px; margin-top: 6px; }
  .group { display: flex; flex-direction: column; gap: 2px; min-width: 200px; }
  .gname { font-size: 11px; font-weight: 700; color: var(--muted); text-transform: uppercase; letter-spacing: 0.03em; border-bottom: 2px solid var(--border); padding-bottom: 2px; }
  .gname { display: flex; align-items: center; gap: 6px; }
  .gtoggle { border: none; background: none; padding: 0; font: inherit; color: inherit; text-transform: inherit; letter-spacing: inherit; cursor: pointer; text-align: left; }
  .gtoggle:hover { color: var(--fg); text-decoration: underline; }
  .gtoggle.alloff { opacity: 0.45; text-decoration: line-through; }
  .focus { border: 1px solid var(--border); background: none; padding: 0 5px; font-size: 11px; line-height: 16px; border-radius: 8px; color: var(--muted); margin-left: auto; }
  .focus.on { background: var(--accent); border-color: var(--accent); color: #fff; }
  .group.focused .gname { color: var(--fg); }
  .group.dimmed { opacity: 0.45; }
  .item { display: flex; align-items: center; gap: 6px; border: none; background: none; padding: 1px 0; font-size: 13px; text-align: left; }
  .item.off { opacity: 0.35; }
  .item.hl { font-weight: 600; background: var(--sel-bg); border-radius: 4px; }
  .item .val { margin-left: auto; padding-left: 10px; font-variant-numeric: tabular-nums; color: var(--muted); }
  .item .val.lost { color: #d62828; font-weight: 600; }
  .hint { color: var(--muted); font-size: 12px; margin: 6px 0 0; }
  .paused { color: var(--accent); margin-right: 6px; }
</style>
