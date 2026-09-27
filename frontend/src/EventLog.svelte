<script>
  import Help from './Help.svelte'
  import { HELP } from './help.js'
  export let events = []
  let showHarmless = false
  let showInfo = true

  const KIND = {
    session: 'Messung', path: 'Route', path_change: 'Routenwechsel', zones: 'Zonen',
    loss: 'Verlust', loss_hop: 'harmlos', spike: 'Latenzspitze', outage_start: 'AUSFALL', outage_end: 'Ausfall Ende',
  }
  const t = (ms) => (ms ? new Date(ms).toLocaleTimeString('de-DE') : '')
  const d = (ms) => new Date(ms).toLocaleDateString('de-DE')

  $: shown = events
    .filter((e) => (showHarmless || e.kind !== 'loss_hop') && (showInfo || e.severity !== 'info' || e.kind === 'loss_hop'))
    .slice()
    .reverse()
  $: harmless = events.filter((e) => e.kind === 'loss_hop').length
</script>

<div class="bar">
  <label><input type="checkbox" bind:checked={showHarmless} /> harmlose Hop-Verluste ({harmless})</label><Help text={HELP.harmless} />
  <label><input type="checkbox" bind:checked={showInfo} /> Infos (Start, Zonen)</label>
  <span class="muted">{shown.length} Einträge, neueste oben</span>
</div>
<div class="log">
  <table>
    <tr><th>Datum</th><th>Beginn</th><th>Ende</th><th>Art</th><th>Zone</th><th>Ereignis</th></tr>
    {#each shown as e (e.id)}
      <tr class="sev-{e.severity}">
        <td>{d(e.t)}</td>
        <td>{t(e.t)}</td>
        <td>{t(e.end)}</td>
        <td><span class="badge k-{e.kind}">{KIND[e.kind] || e.kind}</span></td>
        <td>{e.zone}</td>
        <td class="txt"><b>{e.title}</b>{#if e.detail}<br /><small>{e.detail}</small>{/if}</td>
      </tr>
    {:else}
      <tr><td colspan="6" class="muted">Noch keine Ereignisse.</td></tr>
    {/each}
  </table>
</div>

<style>
  .bar { display: flex; gap: 16px; align-items: center; font-size: 12px; margin-bottom: 6px; flex-wrap: wrap; }
  .log { max-height: 420px; overflow: auto; }
  td { vertical-align: top; }
  td.txt { text-align: left; }
  td:nth-child(-n + 3) { white-space: nowrap; }
  small { color: var(--muted); }
  .sev-crit { background: #fde2e2; }
  .sev-warn { background: #fff6e0; }
  .sev-ok { background: #e9f7ef; }
  .sev-info td { color: var(--muted); }
  .badge { font-size: 11px; padding: 1px 6px; border-radius: 8px; background: #eee; white-space: nowrap; }
  .k-loss, .k-outage_start { background: #d62828; color: #fff; }
  .k-spike { background: #f4a261; color: #fff; }
  .k-path_change { background: #3a5a8c; color: #fff; }
  .k-outage_end { background: #2a9d8f; color: #fff; }
  .muted { color: var(--muted); }
</style>
