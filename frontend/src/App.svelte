<script>
  import { onMount } from 'svelte'
  import { StartMonitoring, StopMonitoring, GetStatus, ListSessions, GetSession, Export, SetHopWatched, SetHopZone, SetHopName, GetVersion, GetLive, SaveCustomPoint, DeleteCustomPoint, AddSuggestedPoints, GetSettings, SaveSettings, DefaultSettings, DeleteSession } from '../wailsjs/go/main/App.js'
  import { EventsOn } from '../wailsjs/runtime/runtime.js'
  import Chart from './Chart.svelte'
  import EventLog from './EventLog.svelte'
  import Diagnosis from './Diagnosis.svelte'
  import IncidentList from './IncidentList.svelte'
  import Settings from './Settings.svelte'
  import { signalFor } from './sound.js'
  import { Timeline } from './timeline.js'
  import { ZONES, ZONE_COLOR, ZONE_LABEL, fmtTime, fmtDur, hopKey, devKey, buildSeries, colorMap, DEFAULT_ZONES } from './zones.js'
  import Help from './Help.svelte'
  import { HELP } from './help.js'

  let tab = 'live'

  // Settings and color scheme.
  let settings = null
  $: zoneDefs = settings?.zones || DEFAULT_ZONES
  $: zoneName = (id) => zoneDefs.find((z) => z.id === id)?.name || id
  $: zoneColor = (id) => zoneDefs.find((z) => z.id === id)?.color || '#888'

  let showSettings = false
  let themeTick = 0
  let liveWindow = 30
  function applyTheme(t) {
    const root = document.documentElement
    if (t === 'light' || t === 'dark') root.dataset.theme = t
    else delete root.dataset.theme
    try { localStorage.setItem('theme', t || 'auto') } catch {}
    themeTick++
  }
  try { applyTheme(localStorage.getItem('theme') || 'auto') } catch {}
  window.matchMedia?.('(prefers-color-scheme: dark)').addEventListener?.('change', () => themeTick++)
  async function saveSettings(s) {
    settings = await SaveSettings(s)
    applyTheme(settings.theme)
    liveWindow = settings.defaultWindowMin
  }
  async function removeSession(id) {
    if (!confirm('Diese Messung mit allen Daten endgültig löschen?')) return
    try {
      await DeleteSession(id)
      sessionData = null
      selected = null
      sessions = (await ListSessions()) || []
      if (sessions.length) await select(sessions[0].id)
    } catch (e) { exportMsg = 'Fehler: ' + e }
  }
  let version = ''
  let target = ''
  try { target = localStorage.getItem('target') || '' } catch {}
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
  $: names = status.names || {}
  $: liveZones = (status.reps || []).map((r) => r.zone)
  // Hops that are zone measuring points appear as zone line.
  $: repZoneByAddr = Object.fromEntries((status.reps || []).map((r) => [r.ip, r.zone]))
  // One chart line per route row, in route order, same color and label as the table.
  $: custom = status.custom || []
  $: colors = colorMap(status.hops || [], status.reps || [], custom)
  $: liveSeries = buildSeries({ hops: status.hops || [], reps: status.reps || [], watched, names, custom })

  // User-defined measuring points (e.g. other devices in the LAN).
  let newHost = '', newName = '', newZone = '', newPing = true, newTcp = false, newPort = 443
  let customBusy = false
  async function addCustom() {
    customBusy = true
    try {
      await SaveCustomPoint(newHost.trim(), newName.trim(), newZone, newPing, newTcp, Number(newPort) || 443)
      newHost = ''; newName = ''; newZone = ''; newPing = true; newTcp = false; newPort = 443
    } catch (e) { error = String(e) }
    customBusy = false
  }
  async function saveCustom(c, patch) {
    const x = { ...c, ...patch }
    try { await SaveCustomPoint(x.host, x.name, x.zone, x.enabled, !!x.tcp, Number(x.port) || 443) } catch (e) { error = String(e) }
  }
  async function removeCustom(c) {
    try { await DeleteCustomPoint(c.host) } catch (e) { error = String(e) }
  }

  // Series keys hidden in the live chart (legend or table checkbox).
  let hidden = new Set(loadHidden())
  function loadHidden() {
    try { return JSON.parse(localStorage.getItem('hiddenSeries') || '[]') } catch { return [] }
  }
  function setHidden(key, hide) {
    hide ? hidden.add(key) : hidden.delete(key)
    hidden = new Set(hidden)
    try { localStorage.setItem('hiddenSeries', JSON.stringify([...hidden])) } catch {}
  }

  // Table checkbox = "line visible in the chart". Hops that are no zone
  // measuring point are probed only while their checkbox is on.
  async function toggleRow(h, on) {
    const zone = repZoneByAddr[h.addr]
    if (zone) return setHidden(zone, !on)
    setHidden(hopKey(h.addr), false)
    try { await SetHopWatched(h.addr, on) } catch (e) { error = String(e) }
  }
  const rowKey = (h) => repZoneByAddr[h.addr] || hopKey(h.addr)
  const rowChecked = (h, watched, hidden) =>
    !hidden.has(rowKey(h)) && (!!repZoneByAddr[h.addr] || watched.includes(h.addr))

  async function rename(addr, name) {
    try { await SetHopName(addr, name) } catch (e) { error = String(e) }
  }

  let histHidden = new Set()

  // Event log and diagnosis of the running session.
  let liveEvents = []
  let liveIncidents = []
  let realLoss = {}
  let liveChart, histChart
  let hoverKey = null // table row under the mouse → highlighted chart line
  let liveDiag = null
  let diagTimer = null
  function refreshDiag() {
    clearTimeout(diagTimer)
    diagTimer = setTimeout(async () => {
      if (!status.sessionId) return
      try {
        const x = await GetLive(status.sessionId)
        liveDiag = x.diagnosis
        liveEvents = x.events || []
        liveIncidents = x.incidents || []
      } catch {}
    }, 1500)
  }

  async function changeZone(addr, zone) {
    try { await SetHopZone(addr, zone === 'auto' ? '' : zone) } catch (e) { error = String(e) }
  }

  onMount(async () => {
    version = await GetVersion()
    try {
      settings = await GetSettings()
      // Last used target is stored in the database, so it survives updates.
      if (settings.lastTarget) target = settings.lastTarget
      applyTheme(settings.theme)
      liveWindow = settings.defaultWindowMin
    } catch {}
    status = await GetStatus()
    if (status.target) target = status.target
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
    EventsOn('realloss', (x) => (realLoss = x))
    EventsOn('incident', (inc) => {
      liveIncidents = [...liveIncidents, inc]
      refreshDiag()
    })
    EventsOn('event', (e) => {
      signalFor(e, settings?.sounds)
      liveEvents = [...liveEvents, e]
      refreshDiag()
    })
    if (status.sessionId) refreshDiag()
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
    stats = {}; outages = []; activeOutage = null; liveEvents = []; liveDiag = null; liveIncidents = []; realLoss = {}
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
    <button class="gear" title="Einstellungen" on:click={() => (showSettings = true)} disabled={!settings}>⚙</button>
    <button class:active={tab === 'live'} on:click={() => (tab = 'live')}>Messung</button>
    <button class:active={tab === 'history'} on:click={openHistory}>Verlauf &amp; Berichte</button>
  </nav>
</header>

<main>
  {#if tab === 'live'}
    <section class="card controls">
      <label>
        <span>Ziel (IP oder Domain)<Help align="left" text={HELP.target} /></span>
        <input bind:value={target} disabled={running} placeholder="z. B. 1.1.1.1 oder google.com"
          on:keydown={(e) => e.key === 'Enter' && !running && start()} />
      </label>
      {#if running}
        <button class="danger" on:click={stop}>Überwachung stoppen</button>
      {:else}
        <button class="primary" on:click={start}>Überwachung starten</button><Help text={HELP.start} />
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
            <h3><span style="color:{zoneColor(z)}">●</span> {zoneName(z)}<Help align={z === 'WAN' ? 'right' : 'left'} text={HELP[z]} /></h3>
            {#if rep}
              <div class="ip">{rep.ip}{rep.direct ? '' : ` (TTL ${rep.ttl})`}</div>
              <div class="big">{fmt(s?.p50Ms)} <small>ms P50</small><Help text={HELP.p50} /></div>
              <table>
                <tr><td>Verlust<Help align="left" text={HELP.loss} /></td><td class:bad={s?.lossPct > 0}>{fmt(s?.lossPct, 2)} % ({(s?.sent ?? 0) - (s?.received ?? 0)}/{s?.sent ?? 0})</td></tr>
                {#if z !== 'WAN' && (s?.sent ?? 0) - (s?.received ?? 0) > 0}
                  <tr title="Nur Verluste, bei denen auch alle folgenden Messpunkte bis zum Ziel nicht antworteten. Der Rest ist ICMP-Drosselung dieses Hops und harmlos.">
                    <td>davon echt (bis Ziel)<Help align="left" text={HELP.realLoss} /></td><td class:bad={realLoss[z] > 0}>{realLoss[z] || 0}</td></tr>
                {/if}
                <tr><td>Jitter (RFC 3550)<Help align="left" text={HELP.jitter} /></td><td>{fmt(s?.jitterMs, 2)} ms</td></tr>
                <tr><td>P95 / P99<Help align="left" text={HELP.p9599} /></td><td>{fmt(s?.p95Ms)} / {fmt(s?.p99Ms)} ms</td></tr>
                <tr><td>Min / Max<Help align="left" text={HELP.minmax} /></td><td>{fmt(s?.minMs)} / {fmt(s?.maxMs)} ms</td></tr>
              </table>
            {:else}
              <div class="ip muted">kein Hop in dieser Zone erkannt</div>
            {/if}
          </div>
        {/each}
      </section>

      {#if liveDiag}<Diagnosis d={liveDiag} live={running} />{/if}

      <section class="card">
        <h2>Latenzverlauf (live)<Help align="left" text={HELP.chart} /></h2>
        <Chart highlight={hoverKey} zones={zoneDefs} bind:this={liveChart} bind:windowMin={liveWindow} theme={themeTick} bands={liveIncidents} live timeline={live} series={liveSeries} {hidden} onToggle={(k, show) => setHidden(k, !show)} version={liveVersion} />
      </section>

      <div class="two">
        <section class="card">
          <h2>Störungen – wer hat wann nicht geantwortet?<Help align="left" text={HELP.incidents} /></h2>
          <IncidentList zones={zoneDefs} incidents={liveIncidents} onSelect={(i) => liveChart?.zoomTo(i.t - 60000, i.end + 60000)} />
        </section>
        <section class="card">
          <h2>Ereignisprotokoll<Help align="left" text={HELP.events} /></h2>
          <EventLog events={liveEvents} />
        </section>
        <section class="card">
          <h2>Route<Help align="left" text={HELP.route} /></h2>
          <table class="route">
            <tr><th>TTL<Help align="left" text={HELP.ttl} /></th><th>Adresse<Help text={HELP.addr} /></th><th>Name<Help text={HELP.name} /></th><th>Zone<Help text={HELP.zone} /></th><th>RTT<Help text={HELP.rtt} /></th><th>Diagramm<Help align="right" text={HELP.diagram} /></th></tr>
            {#each status.hops || [] as h}
              <tr on:mouseenter={() => h.responsive && (hoverKey = rowKey(h))} on:mouseleave={() => (hoverKey = null)}>
                <td>{h.ttl}</td>
                <td>{h.responsive ? h.addr : '* (filtert ICMP – kein Fehler)'}</td>
                <td>
                  {#if h.responsive}
                    <input class="name" value={names[h.addr] || ''} placeholder="optional"
                      on:change={(e) => rename(h.addr, e.target.value)}
                      on:keydown={(e) => e.key === 'Enter' && e.target.blur()}
                      title="Eigener Name für diesen Hop (wird pro Adresse gespeichert, erscheint im Diagramm und Bericht)" />
                  {/if}
                </td>
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
                  {#if h.responsive}
                    <label class="toggle" title={repZoneByAddr[h.addr] ? `Messpunkt der Zone ${repZoneByAddr[h.addr]}` : 'Eigene Linie im Diagramm (wird nur gemessen, solange aktiv)'}>
                      <input type="checkbox" checked={rowChecked(h, watched, hidden)} on:change={(e) => toggleRow(h, e.target.checked)} />
                      <span class="dot" style="background:{colors[rowKey(h)]}"></span>
                    </label>
                  {/if}
                </td>
              </tr>
            {/each}
          </table>
          <p class="muted small">Zone ändern: gilt sofort und wird für diesen Hop dauerhaft gemerkt. „Auto“ stellt die automatische Einteilung wieder her.</p>

          <h2 class="sub">Weitere Messpunkte (manuell)<Help align="left" text={HELP.custom} /></h2>
          <p class="muted small">Beliebige Geräte oder Adressen zusätzlich jede Sekunde anpingen, z. B. ein zweiter Router/Modem, Repeater, NAS oder ein anderer Server. Werden dauerhaft gespeichert und bei jeder Messung mitgemessen.</p>
          <table class="route">
            <tr>
              <th>Adresse / Host<Help align="left" text={HELP.customAuto} /></th><th>Name<Help text={HELP.name} /></th><th>Zone<Help text={HELP.customZone} /></th>
              <th class="c">Ping<Help text={HELP.pingSwitch} /></th><th class="c">TCP-Check<Help text={HELP.tcp} /></th><th></th>
            </tr>
            {#each custom as c (c.host)}
              <tr class:inactive={!c.enabled && !c.tcp} on:mouseenter={() => (hoverKey = c.enabled ? devKey(c.host) : 'tcp:' + c.host)} on:mouseleave={() => (hoverKey = null)}>
                <td>{c.host}{#if c.ip && c.ip !== c.host}<br /><small class="muted">{c.ip}</small>{/if}{#if c.error}<br /><small class="bad">{c.error}</small>{/if}</td>
                <td><input class="name" value={c.name} placeholder="optional" on:change={(e) => saveCustom(c, { name: e.target.value })}
                  on:keydown={(e) => e.key === 'Enter' && e.target.blur()} /></td>
                <td>
                  <select style="color:{zoneColor(c.zone)}" value={c.zone} on:change={(e) => saveCustom(c, { zone: e.target.value })}>
                    {#each zoneDefs as z}<option value={z.id}>{z.name}</option>{/each}
                  </select>
                </td>
                <td class="c nowrap">
                  <label class="switch" title="Ping messen an/aus"><input type="checkbox" checked={c.enabled} on:change={(e) => saveCustom(c, { enabled: e.target.checked })} /><span></span></label>
                  <label class="toggle" class:off={!c.enabled} title="Linie im Diagramm anzeigen">
                    <input type="checkbox" disabled={!c.enabled} checked={c.enabled && !hidden.has(devKey(c.host))} on:change={(e) => setHidden(devKey(c.host), !e.target.checked)} />
                    <span class="dot" style="background:{colors[devKey(c.host)]}"></span>
                  </label>
                </td>
                <td class="c nowrap" on:mouseenter={() => c.tcp && (hoverKey = 'tcp:' + c.host)} on:mouseleave={() => (hoverKey = c.enabled ? devKey(c.host) : null)}>
                  <label class="switch" title="TCP-Check an/aus"><input type="checkbox" checked={c.tcp} on:change={(e) => saveCustom(c, { tcp: e.target.checked })} /><span></span></label>
                  <input class="port" type="number" min="1" max="65535" value={c.port || 443} disabled={!c.tcp} on:change={(e) => saveCustom(c, { port: e.target.value })} title="Port" />
                  <label class="toggle" class:off={!c.tcp} title="Linie im Diagramm anzeigen">
                    <input type="checkbox" disabled={!c.tcp} checked={c.tcp && !hidden.has('tcp:' + c.host)} on:change={(e) => setHidden('tcp:' + c.host, !e.target.checked)} />
                    <span class="dot dotted" style="border-color:{colors['tcp:' + c.host]}"></span>
                  </label>
                </td>
                <td><button class="del" title="Messpunkt entfernen" on:click={() => removeCustom(c)}>✕</button></td>
              </tr>
            {/each}
            <tr>
              <td><input class="name" bind:value={newHost} placeholder="IP oder Hostname" on:keydown={(e) => e.key === 'Enter' && newHost && addCustom()} /></td>
              <td><input class="name" bind:value={newName} placeholder="Name (optional)" /></td>
              <td>
                <select bind:value={newZone}>
                  <option value="">Auto</option>
                  {#each zoneDefs as z}<option value={z.id}>{z.name}</option>{/each}
                </select>
              </td>
              <td class="c"><label class="switch" title="Ping messen"><input type="checkbox" bind:checked={newPing} /><span></span></label></td>
              <td class="c nowrap">
                <label class="switch" title="TCP-Check"><input type="checkbox" bind:checked={newTcp} /><span></span></label>
                <input class="port" type="number" min="1" max="65535" bind:value={newPort} disabled={!newTcp} title="Port" />
              </td>
              <td><button class="primary" disabled={!newHost.trim() || customBusy} on:click={addCustom}>Hinzufügen</button></td>
            </tr>
          </table>
          {#if ['1.1.1.1', '9.9.9.9', '8.8.8.8'].some((h) => !custom.find((c) => c.host === h))}
            <button class="suggest" on:click={() => AddSuggestedPoints().catch((e) => (error = String(e)))}>+ Vorschläge 1.1.1.1 / 9.9.9.9 / 8.8.8.8 hinzufügen</button>
          {/if}
        </section>
      </div>
    {/if}
  {:else}
    <div class="history">
      <aside class="card">
        <h2>Sitzungen<Help align="left" text={HELP.sessions} /></h2>
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
                <button on:click={() => doExport('html')}>Bericht (HTML/PDF)</button><Help text={HELP.exportHtml} />
                <button on:click={() => doExport('csv')}>Rohdaten (CSV)</button><Help text={HELP.exportCsv} />
                <button on:click={() => doExport('events')}>Ereignisse (CSV)</button>
                <button class="del-session" title="Diese Messung löschen" on:click={() => removeSession(r.session.id)}>🗑</button><Help align="right" text={HELP.exportEvents} />
              </span>
            </div>
            <p>{fmtTime(r.session.startedAt)} – {fmtTime(r.session.endedAt)} · Dauer {fmtDur(r.durationSec)} · {r.pathChanges} Routenwechsel</p>
            <Diagnosis d={r.diagnosis} />
            {#if exportMsg}<p class="muted">{exportMsg}</p>{/if}
          </section>
          <section class="card">
            <h2>Kennzahlen</h2>
            <table>
              <tr><th>Zone</th><th>Probes</th><th>Verlust %<Help text={HELP.loss} /></th><th>Min</th><th>Ø</th><th>P50</th><th>P95<Help text={HELP.p9599} /></th><th>P99</th><th>Max</th><th>Jitter<Help text={HELP.jitter} /></th><th>&gt;100 ms<Help text={HELP.spikes} /></th><th>Ausfallzeit<Help align="right" text={HELP.outageTime} /></th></tr>
              {#each r.zones || [] as z}
                <tr><td style="color:{ZONE_COLOR[z.zone]}">{z.zone}</td><td>{z.summary.sent}</td><td>{fmt(z.summary.lossPct, 2)}</td>
                  <td>{fmt(z.summary.minMs)}</td><td>{fmt(z.summary.avgMs)}</td><td>{fmt(z.summary.p50Ms)}</td><td>{fmt(z.summary.p95Ms)}</td>
                  <td>{fmt(z.summary.p99Ms)}</td><td>{fmt(z.summary.maxMs)}</td><td>{fmt(z.summary.jitterMs, 2)}</td><td>{z.spikesOver100}</td>
                  <td>{fmtDur(r.outageSeconds?.[z.zone] || 0)}</td></tr>
              {/each}
            </table>
          </section>
          <section class="card">
            <h2>Latenzverlauf<Help align="left" text={HELP.chart} /></h2>
            <Chart zones={zoneDefs} bind:this={histChart} theme={themeTick} bands={r.incidents || []} timeline={hist} series={buildSeries({ hops: r.hops || [], reps: [], names: r.hops ? Object.fromEntries(r.hops.map((h) => [h.addr, h.name || ''])) : {}, keys: (sessionData.series || []).map((x) => x.zone), custom: (status.custom || []).map((c) => ({ ...c, enabled: true })) })}
              hidden={histHidden} onToggle={(k, show) => { show ? histHidden.delete(k) : histHidden.add(k); histHidden = new Set(histHidden) }} version={histVersion} />
          </section>
          <section class="card">
            <h2>Störungen – wer hat wann nicht geantwortet?<Help align="left" text={HELP.incidents} /></h2>
            <IncidentList zones={zoneDefs} incidents={r.incidents || []} onSelect={(i) => histChart?.zoomTo(i.t - 60000, i.end + 60000)} />
          </section>
          <section class="card">
            <h2>Ereignisprotokoll<Help align="left" text={HELP.events} /></h2>
            <EventLog events={r.events || []} />
          </section>
        </div>
      {/if}
    </div>
  {/if}
</main>

{#if showSettings && settings}
  <Settings {settings} onSave={saveSettings} onClose={() => (showSettings = false)} onDefaults={DefaultSettings} onPreviewTheme={applyTheme} />
{/if}

<style>
  header { display: flex; align-items: center; justify-content: space-between; padding: 10px 20px; background: #1f2a44; color: #fff; }
  header h1 { font-size: 18px; margin: 0; }
  .ver { font-size: 12px; font-weight: 400; color: #cfd6e6; margin-left: 6px; }
  nav button { background: transparent; color: #cfd6e6; border: none; }
  nav button.active { color: #fff; border-bottom: 2px solid #fff; border-radius: 0; }
  nav { display: flex; align-items: center; gap: 4px; }
  nav .gear { order: 3; font-size: 18px; padding: 2px 10px; margin-left: 8px; }
  nav .gear:hover { color: #fff; }
  .del-session { padding: 6px 10px; }
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
  .two { display: grid; grid-template-columns: 1fr; gap: 14px; }
  .history { display: grid; grid-template-columns: 240px 1fr; gap: 14px; align-items: start; }
  .session { display: block; width: 100%; text-align: left; margin-bottom: 6px; }
  .session.active { border-color: var(--accent); background: var(--sel-bg); }
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
  .row { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
  .route select { padding: 2px 4px; font-size: 12px; }
  .route .name { width: 130px; padding: 2px 6px; font-size: 12px; }
  .sub { margin-top: 18px; }
  .del { padding: 1px 8px; font-size: 12px; }
  td.nowrap { white-space: nowrap; }
  th.c, td.c { text-align: center; }
  tr.inactive td { opacity: 0.6; }
  .suggest { margin-top: 8px; font-size: 12px; padding: 4px 10px; }
  .toggle.off { opacity: 0.35; }
  .dot.dotted { background: none; border: 2px dotted; width: 7px; height: 7px; }
  .switch { position: relative; display: inline-block; width: 30px; height: 16px; vertical-align: middle; margin-right: 6px; }
  .switch input { opacity: 0; width: 0; height: 0; }
  .switch span { position: absolute; inset: 0; background: var(--none-cell); border-radius: 16px; transition: 0.15s; cursor: pointer; }
  .switch span::before { content: ''; position: absolute; width: 12px; height: 12px; left: 2px; top: 2px; background: #fff; border-radius: 50%; transition: 0.15s; }
  .switch input:checked + span { background: var(--accent); }
  .switch input:checked + span::before { transform: translateX(14px); }
  .port { width: 70px; padding: 2px 6px; font-size: 12px; margin-left: 4px; }
  .toggle { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
  .dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
  .small { font-size: 12px; margin: 6px 0 0; }
</style>
