<script>
  import { onDestroy } from 'svelte'
  import uPlot from 'uplot'

  export let readings = [] // docsis.Snapshot[], oldest first
  export let bands = [] // incidents [{t, end, class}]
  export let theme = 0

  // Target ranges (Vodafone pNTP via DOCSight "VFKD"); the chart shows the
  // "good" band of the most common modulation as red dashed limits.
  const LIMITS = {
    ds30: [-4, 13], // 256QAM
    ds31: [-12, 12], // OFDM
    us30: [41.1, 47], // SC-QAM
    us31: [44.1, 47], // OFDMA
    snr30: 33, // 256QAM
    mer31: 27, // OFDM
  }

  const css = (n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim()
  const minMax = (chs, f) => {
    const v = chs.map(f).filter((x) => x !== null && x !== undefined && !Number.isNaN(x))
    return v.length ? [Math.min(...v), Math.max(...v)] : [null, null]
  }

  // Series per chart: [label, color, values], plus limits [lo, hi].
  $: xs = readings.map((r) => r.t / 1000)
  $: charts = [
    {
      title: 'Downstream-Pegel (dBmV)',
      hint: 'Empfangspegel der Kanäle. Liegt eine Linie außerhalb der roten Grenzen, ist das Signal zu schwach oder zu stark.',
      series: [
        ['DOCSIS 3.0 min', '#1f77b4', readings.map((r) => minMax(r.ds.filter((c) => c.version === '3.0'), (c) => c.power)[0])],
        ['DOCSIS 3.0 max', '#6baed6', readings.map((r) => minMax(r.ds.filter((c) => c.version === '3.0'), (c) => c.power)[1])],
        ['DOCSIS 3.1 min', '#2ca02c', readings.map((r) => minMax(r.ds.filter((c) => c.version === '3.1'), (c) => c.power)[0])],
      ],
      limits: [LIMITS.ds30],
    },
    {
      title: 'Signalqualität SNR/MER (dB)',
      hint: 'Signal-Rausch-Abstand – je höher, desto besser. Unter der roten Linie wird die Übertragung fehleranfällig.',
      series: [
        ['SNR 3.0 (schlechtester Kanal)', '#9467bd', readings.map((r) => minMax(r.ds.filter((c) => c.version === '3.0' && c.snr > 0), (c) => c.snr)[0])],
        ['MER 3.1 (schlechtester Kanal)', '#17becf', readings.map((r) => minMax(r.ds.filter((c) => c.version === '3.1' && c.snr > 0), (c) => c.snr)[0])],
      ],
      limits: [[LIMITS.snr30, null], [LIMITS.mer31, null]],
    },
    {
      title: 'Upstream-Sendepegel (dBmV)',
      hint: 'Mit welcher Leistung die FRITZ!Box senden muss. Zu hoch (über der roten Linie) heißt: Die Leitung dämpft zu stark – typischer Fall für den Anbieter-Techniker.',
      series: [
        ['DOCSIS 3.0 max', '#ff7f0e', readings.map((r) => minMax(r.us.filter((c) => c.version === '3.0'), (c) => c.power)[1])],
        ['DOCSIS 3.0 min', '#ffbb78', readings.map((r) => minMax(r.us.filter((c) => c.version === '3.0'), (c) => c.power)[0])],
        ['DOCSIS 3.1', '#8c564b', readings.map((r) => minMax(r.us.filter((c) => c.version === '3.1'), (c) => c.power)[1])],
      ],
      limits: [LIMITS.us30],
    },
    {
      title: 'Fehler je Abfrage',
      hint: 'Neue nicht korrigierbare Fehler seit der vorherigen Abfrage: Hier gehen Daten verloren. Jeder Wert über 0 (rote Linie) ist ein Problem der Leitung.',
      series: [['nicht korrigierbar (neu)', '#d62828', readings.map((r) => r.nonCorrDelta ?? 0)]],
      limits: [[null, 0]],
      bars: true,
    },
  ]

  let els = []
  let plots = []

  // Red dashed limit lines; everything beyond them is tinted red.
  function limitPlugin(limits) {
    return {
      hooks: {
        drawAxes: (u) => {
          const ctx = u.ctx
          const { left, top, width, height } = u.bbox
          ctx.save()
          for (const b of bands) {
            const x0 = Math.max(u.valToPos(b.t / 1000, 'x', true), left)
            const x1 = Math.min(u.valToPos(b.end / 1000, 'x', true), left + width)
            if (x1 >= x0) {
              ctx.fillStyle = 'rgba(214,40,40,0.10)'
              ctx.fillRect(x0, top, Math.max(2, x1 - x0), height)
            }
          }
          ctx.fillStyle = 'rgba(214,40,40,0.08)'
          for (const [lo, hi] of limits) {
            if (lo !== null) {
              const y = u.valToPos(lo, 'y', true)
              if (y < top + height) ctx.fillRect(left, y, width, top + height - y)
            }
            if (hi !== null) {
              const y = u.valToPos(hi, 'y', true)
              if (y > top) ctx.fillRect(left, top, width, y - top)
            }
          }
          ctx.restore()
        },
        draw: (u) => {
          const ctx = u.ctx
          const { left, top, width, height } = u.bbox
          ctx.save()
          ctx.strokeStyle = '#d62828'
          ctx.lineWidth = 1.5 * devicePixelRatio
          ctx.setLineDash([6 * devicePixelRatio, 4 * devicePixelRatio])
          for (const pair of limits) {
            for (const v of pair) {
              if (v === null) continue
              const y = u.valToPos(v, 'y', true)
              if (y < top || y > top + height) continue
              ctx.beginPath()
              ctx.moveTo(left, y)
              ctx.lineTo(left + width, y)
              ctx.stroke()
            }
          }
          ctx.restore()
        },
      },
    }
  }

  function build() {
    plots.forEach((p) => p?.destroy())
    plots = []
    charts.forEach((c, i) => {
      const el = els[i]
      if (!el) return
      const lims = c.limits.flat().filter((v) => v !== null)
      const data = [xs, ...c.series.map((s) => s[2])]
      plots[i] = new uPlot(
        {
          width: el.clientWidth || 400,
          height: 170,
          plugins: [limitPlugin(c.limits)],
          legend: { show: true, live: true },
          scales: {
            x: { time: true },
            y: {
              range: (u, min, max) => {
                const lo = Math.min(min ?? 0, ...lims)
                const hi = Math.max(max ?? 1, ...lims)
                const pad = Math.max(1, (hi - lo) * 0.15)
                return [c.bars ? 0 : lo - pad, hi + pad]
              },
            },
          },
          axes: [
            { stroke: css('--axis'), grid: { stroke: css('--grid') }, ticks: { stroke: css('--grid') }, space: 80,
              values: (u, vals) => vals.map((v) => new Date(v * 1000).toLocaleTimeString('de-DE', { hour: '2-digit', minute: '2-digit' })) },
            { stroke: css('--axis'), grid: { stroke: css('--grid') }, ticks: { stroke: css('--grid') }, size: 44 },
          ],
          series: [
            { value: (u, v) => (v == null ? '–' : new Date(v * 1000).toLocaleTimeString('de-DE')) },
            ...c.series.map(([label, color]) => ({
              label,
              stroke: color,
              width: 2,
              fill: c.bars ? color + '55' : undefined,
              paths: c.bars ? uPlot.paths.bars({ size: [0.6, 12] }) : undefined,
              points: { show: true, size: 5 },
              spanGaps: true,
              value: (u, v) => (v == null ? '–' : c.bars ? String(v) : v.toFixed(1)),
            })),
          ],
        },
        data,
        el,
      )
    })
  }

  let builtFor = ''
  $: sig = readings.length + '|' + (readings[readings.length - 1]?.t || 0) + '|' + theme + '|' + bands.length
  $: if (els.length && readings.length && sig !== builtFor) {
    builtFor = sig
    queueMicrotask(build)
  }
  onDestroy(() => plots.forEach((p) => p?.destroy()))
</script>

{#if readings.length}
  <div class="grid">
    {#each charts as c, i}
      <div class="cell">
        <div class="t" title={c.hint}>{c.title} <span class="q">?</span></div>
        <div bind:this={els[i]} class="plot"></div>
      </div>
    {/each}
  </div>
  <p class="note"><span class="lim"></span> rot gestrichelt = Soll-Grenzen (Vodafone-Spezifikation, bei gemischten Modulationen für 256QAM/SC-QAM), rötlich = außerhalb des Sollbereichs, rote Flächen = Störungen. Mehr Abfragen = mehr Punkte; das Intervall ist in ⚙ einstellbar.</p>
{/if}

<style>
  .grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  @media (max-width: 1000px) { .grid { grid-template-columns: minmax(0, 1fr); } }
  .cell { min-width: 0; border: 1px solid var(--border); border-radius: 8px; padding: 6px 8px; }
  .t { font-size: 13px; font-weight: 600; margin-bottom: 2px; cursor: help; }
  .q { font-size: 10px; color: var(--muted); border: 1px solid var(--border); border-radius: 50%; padding: 0 4px; }
  .plot { width: 100%; }
  .plot :global(.u-legend) { font-size: 11px; }
  .note { font-size: 12px; color: var(--muted); margin: 6px 0 0; }
  .lim { display: inline-block; width: 22px; border-top: 2px dashed #d62828; vertical-align: middle; }
</style>
