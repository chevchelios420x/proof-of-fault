<script>
  import { onMount } from 'svelte'
  import { StartMonitoring, StopMonitoring, GetStatus, ListSessions, GetSession, Export, SetHopWatched, SetHopZone, GetVersion } from '../wailsjs/go/main/App.js'
  import { EventsOn } from '../wailsjs/runtime/runtime.js'
  import Chart from './Chart.svelte'
  import { Timeline } from './timeline.js'
  import { ZONES, ZONE_COLOR, ZONE_LABEL, fmtTime, fmtDur, hopKey, seriesColor } from './zones.js'

  let tab = 'live'
  let version = ''
  let target = localStorage.getItem('target') || '1.1.1.1'
  let status = { state: 'idle', reps: [], hops: [] }
  let stats = {}
  let outages = [] // live outage log
  let activeOutage = null
  let error = ''
  let startedAt = 0
  let now = Date.now()

  const live = new Timeline()
  let liveVersion = 0

  let sessions = []
  let selected = null
  let sessionData = null
  const hist = new Timeline()
  let histVersion = 0
  let exportMsg = ''

  $: running = ['resolving', 'discovering', 'running'].includes(status.state)
  $: watched = status.watched || []
  $: liveZones = (status.reps || []).map((r) => r.zone)
  $: liveKeys = [...liveZones, ...(status.hops || []).filter((h) => h.responsive && watched.includes(h.addr)).map((h) => hopKey(h.addr))]

  function loadWatched() {
    try { return JSON.parse(localStorage.getItem('watchedHops') || '[]') } catch { return [] }
  }

  async function toggleHop(addr, on) {
    const w = new Set(loadWatched())
    on ? w.add(addr) : w.delete(addr)
    localStorage.setItem('watchedHops', JSON.stringify([...w]))
    try { await SetHopWatched(addr, on) } catch (e) { error = String(e) }
  }

  async function changeZone(addr, zone) {
    try { await SetHopZone(addr, zone === 'auto' ? '' : zone) } catch (e) { error = String(e) }
  }

  onMount(async () => {
    version = await GetVersion()
    status = await GetStatus()
    // Restore the remembered hop selection (backend keeps it only in memory).
    for (const a of loadWatched()) if (!(status.watched || []).includes(a)) await SetHopWatched(a, true)
    if (status.state === 'error') error = status.message
    EventsOn('status', (s) => {
      status = s
      if (s.state === 'error') error = s.message
    })
    EventsOn('samples', (batch) => {
      for (const s of batch) live.add(s.zone, s.t, s.rttMs)
      liveVersion++
    })
    EventsOn('stats', (s) => (stats = s))
    EventsOn('outage', (o) => {
      if (o.active) {
        activeOutage = o
        outages = [{ zone: o.zone, start: o.since, end: 0 }, ...outages]
      } else {
        activeOutage = null
        if (outages[0] && !outages[0].end) outages[0].end = o.since
        outages = outages
      }
    })
    setInterval(() => (now = Date.now()), 1000)
  })

  async function start() {
    error = ''
    localStorage.setItem('target', target)
    live.reset(); liveVersion++
    stats = {}; outages = []; activeOutage = null
    startedAt = Date.now()
    try { await StartMonitoring(target.trim()) } catch (e) { error = String(e) }
  }

  async function stop() {
    await StopMonitoring()
  }

  async function openHistory() {
    tab = 'history'
    sessions = (await ListSessions()) || []
    if (!selected && sessions.length) await select(sessions[0].id)
  }

  async function select(id) {
    selected = id
    exportMsg = ''
    sessionData = await GetSession(id)
    hist.loadSeries(sessionData.series || [])
    histVersion++
  }

  async function doExport(fmt) {
    try {
      const p = await Export(selected, fmt)
      exportMsg = p ? `Gespeichert: ${p}` : ''
    } catch (e) { exportMsg = 'Fehler: ' + e }
  }

  const fmt = (v, d = 1) => (v === undefined || v === null ? '–' : Number(v).toFixed(d))
</script>

<header>
  <h1>proof-of-fault <span class="ver">{version}</span></h1>
  <nav>
    <button class:active={tab === 'live'} on:click={() => (tab = 'live')}>Messung</button>
    <button class:active={tab === 'history'} on:click={openHistory}>Verlauf &amp; Berichte</button>
  </nav>
</header>

<main>
  {#if tab === 'live'}
    <section class="card controls">
      <label>Ziel (IP oder Domain)
        <input bind:value={target} disabled={running} placeholder="z. B. 1.1.1.1 oder google.com"
          on:keydown={(e) => e.key === 'Enter' && !running && start()} />
      </label>
      {#if running}
        <button class="danger" on:click={stop}>Überwachung stoppen</button>
      {:else}
        <button class="primary" on:click={start}>Überwachung starten</button>
      {/if}
      <span class="state">
        {#if status.state === 'resolving'}Löse Namen auf …
        {:else if status.state === 'discovering'}{status.message}
        {:else if status.state === 'running'}Läuft seit {fmtDur((now - startedAt) / 1000)} · Ziel {status.targetIp} · Sitzung #{status.sessionId}
        {:else}Bereit{/if}
      </span>
    </section>

    {#if error}<div class="card err">{error}</div>{/if}

    {#if activeOutage}
      <div class="card alarm">
        AUSFALL seit {new Date(activeOutage.since).toLocaleTimeString('de-DE')} – Ursache in Zone <b>{activeOutage.zone}</b>
        ({fmtDur((now - activeOutage.since) / 1000)})
      </div>
    {/if}

    {#if liveZones.length}
      <section class="zones">
        {#each ZONES as z}
          {@const rep = status.reps.find((r) => r.zone === z)}
          {@const s = stats[z]}
          <div class="card zone" style="border-top: 4px solid {ZONE_COLOR[z]}">
            <h3>{ZONE_LABEL[z]}</h3>
            {#if rep}
              <div class="ip">{rep.ip}{rep.direct ? '' : ` (TTL ${rep.ttl})`}</div>
              <div class="big">{fmt(s?.p50Ms)} <small>ms P50</small></div>
              <table>
                <tr><td>Verlust</td><td class:bad={s?.lossPct > 0}>{fmt(s?.lossPct, 2)} % ({(s?.sent ?? 0) - (s?.received ?? 0)}/{s?.sent ?? 0})</td></tr>
                <tr><td>Jitter (RFC 3550)</td><td>{fmt(s?.jitterMs, 2)} ms</td></tr>
                <tr><td>P95 / P99</td><td>{fmt(s?.p95Ms)} / {fmt(s?.p99Ms)} ms</td></tr>
                <tr><td>Min / Max</td><td>{fmt(s?.minMs)} / {fmt(s?.maxMs)} ms</td></tr>
              </table>
            {:else}
              <div class="ip muted">kein Hop in dieser Zone erkannt</div>
            {/if}
          </div>
        {/each}
      </section>

      <section class="card">
        <h2>Latenzverlauf (live)</h2>
        <Chart timeline={live} zones={liveKeys} hops={status.hops || []} version={liveVersion} />
      </section>

      <div class="two">
        <section class="card">
          <h2>Ausfälle dieser Sitzung</h2>
          {#if outages.length}
            <table>
              <tr><th>Zone</th><th>Beginn</th><th>Ende</th><th>Dauer</th></tr>
              {#each outages as o}
                <tr><td>{o.zone}</td><td>{fmtTime(o.start)}</td><td>{fmtTime(o.end)}</td><td>{fmtDur(((o.end || now) - o.start) / 1000)}</td></tr>
              {/each}
            </table>
          {:else}<p class="muted">Bisher keine.</p>{/if}
        </section>
        <section class="card">
          <h2>Route</h2>
          <table class="route">
            <tr><th>TTL</th><th>Adresse</th><th>Zone</th><th>RTT</th><th title="Eigene Latenzlinie im Diagramm">Diagramm</th></tr>
            {#each status.hops || [] as h}
              <tr>
                <td>{h.ttl}</td>
                <td>{h.responsive ? h.addr : '* (filtert ICMP – kein Fehler)'}</td>
                <td>
                  {#if h.responsive}
                    <select style="color:{ZONE_COLOR[h.zone]}" value={h.manual ? h.zone : 'auto'}
                      on:change={(e) => changeZone(h.addr, e.target.value)}
                      title="Zone dieses Hops. Die Auswahl wird pro Hop-Adresse gespeichert.">
                      <option value="auto">{h.manual ? 'Auto' : `Auto (${h.zone})`}</option>
                      {#each ZONES as z}<option value={z}>{z}</option>{/each}
                    </select>
                  {:else}<span style="color:{ZONE_COLOR[h.zone]}">{h.zone}</span>{/if}
                </td>
                <td>{h.responsive ? fmt(h.rttMs) + ' ms' : ''}</td>
                <td>
                  {#if h.responsive && h.addr !== status.targetIp}
                    <label class="toggle">
                      <input type="checkbox" checked={watched.includes(h.addr)} on:change={(e) => toggleHop(h.addr, e.target.checked)} />
                      <span class="dot" style="background:{seriesColor(hopKey(h.addr))}"></span>
                    </label>
                  {/if}
                </td>
              </tr>
            {/each}
          </table>
          <p class="muted small">Zone ändern: gilt sofort und wird für diesen Hop dauerhaft gemerkt. „Auto“ stellt die automatische Einteilung wieder her.</p>
        </section>
      </div>
    {/if}
  {:else}
    <div class="history">
      <aside class="card">
        <h2>Sitzungen</h2>
        {#each sessions as s}
          <button class="session" class:active={s.id === selected} on:click={() => select(s.id)}>
            #{s.id} {s.target}<br /><small>{fmtTime(s.startedAt)}</small>
          </button>
        {:else}<p class="muted">Noch keine Messungen.</p>{/each}
      </aside>
      {#if sessionData}
        {@const r = sessionData.report}
        <div class="detail">
          <section class="card">
            <div class="row">
              <h2>Sitzung #{r.session.id}: {r.session.target} ({r.session.targetIp})</h2>
              <span>
                <button on:click={() => doExport('html')}>Bericht (HTML/PDF)</button>
                <button on:click={() => doExport('csv')}>Rohdaten (CSV)</button>
              </span>
            </div>
            <p>{fmtTime(r.session.startedAt)} – {fmtTime(r.session.endedAt)} · Dauer {fmtDur(r.durationSec)} · {r.pathChanges} Routenwechsel</p>
            <p class="verdict">{r.verdict}</p>
            {#if exportMsg}<p class="muted">{exportMsg}</p>{/if}
          </section>
          <section class="card">
            <h2>Kennzahlen</h2>
            <table>
              <tr><th>Zone</th><th>Probes</th><th>Verlust %</th><th>Min</th><th>Ø</th><th>P50</th><th>P95</th><th>P99</th><th>Max</th><th>Jitter</th><th>&gt;100 ms</th><th>Ausfallzeit</th></tr>
              {#each r.zones || [] as z}
                <tr><td style="color:{ZONE_COLOR[z.zone]}">{z.zone}</td><td>{z.summary.sent}</td><td>{fmt(z.summary.lossPct, 2)}</td>
                  <td>{fmt(z.summary.minMs)}</td><td>{fmt(z.summary.avgMs)}</td><td>{fmt(z.summary.p50Ms)}</td><td>{fmt(z.summary.p95Ms)}</td>
                  <td>{fmt(z.summary.p99Ms)}</td><td>{fmt(z.summary.maxMs)}</td><td>{fmt(z.summary.jitterMs, 2)}</td><td>{z.spikesOver100}</td>
                  <td>{fmtDur(r.outageSeconds?.[z.zone] || 0)}</td></tr>
              {/each}
            </table>
          </section>
          <section class="card">
            <h2>Latenzverlauf</h2>
            <Chart timeline={hist} zones={(sessionData.series || []).map((x) => x.zone)} hops={r.hops || []} version={histVersion} />
          </section>
          <section class="card">
            <h2>Ausfälle</h2>
            {#if r.outages?.length}
              <table>
                <tr><th>Zone</th><th>Beginn</th><th>Ende</th><th>Dauer</th></tr>
                {#each r.outages as o}<tr><td>{o.zone}</td><td>{fmtTime(o.start)}</td><td>{fmtTime(o.end)}</td><td>{fmtDur(o.seconds)}</td></tr>{/each}
              </table>
            {:else}<p class="muted">Keine.</p>{/if}
          </section>
        </div>
      {/if}
    </div>
  {/if}
</main>

<style>
  header { display: flex; align-items: center; justify-content: space-between; padding: 10px 20px; background: #1f2a44; color: #fff; }
  header h1 { font-size: 18px; margin: 0; }
  .ver { font-size: 12px; font-weight: 400; color: #cfd6e6; margin-left: 6px; }
  nav button { background: transparent; color: #cfd6e6; border: none; }
  nav button.active { color: #fff; border-bottom: 2px solid #fff; border-radius: 0; }
  main { padding: 16px 20px; display: flex; flex-direction: column; gap: 14px; }
  h2 { font-size: 15px; margin: 0 0 8px; }
  h3 { font-size: 13px; margin: 0 0 4px; color: var(--muted); }
  .controls { display: flex; align-items: end; gap: 12px; flex-wrap: wrap; }
  .controls label { display: flex; flex-direction: column; font-size: 12px; color: var(--muted); gap: 4px; }
  .controls input { width: 280px; }
  .state { color: var(--muted); font-size: 13px; padding-bottom: 7px; }
  .err { border-color: var(--bad); color: var(--bad); }
  .alarm { background: var(--bad); color: #fff; border: none; font-weight: 600; }
  .zones { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; }
  .zone .ip { font-family: Consolas, monospace; font-size: 13px; }
  .zone .big { font-size: 28px; font-weight: 600; margin: 6px 0; }
  .zone .big small { font-size: 12px; font-weight: 400; color: var(--muted); }
  .bad { color: var(--bad); font-weight: 600; }
  .muted { color: var(--muted); }
  .two { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  .history { display: grid; grid-template-columns: 240px 1fr; gap: 14px; align-items: start; }
  .session { display: block; width: 100%; text-align: left; margin-bottom: 6px; }
  .session.active { border-color: var(--accent); background: #eef2fa; }
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
  .row { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
  .route select { padding: 2px 4px; font-size: 12px; }
  .toggle { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
  .dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
  .small { font-size: 12px; margin: 6px 0 0; }
  .verdict { background: #fff4e5; border-left: 4px solid var(--isp); padding: 8px 12px; margin: 8px 0 0; }
</style>
