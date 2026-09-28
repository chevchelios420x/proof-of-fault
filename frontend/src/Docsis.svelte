<script>
  import Help from './Help.svelte'
  export let readings = [] // docsis.Snapshot[], oldest first
  export let live = false

  const ST = { good: 'in Ordnung', warning: 'auffällig', critical: 'kritisch' }
  const f1 = (v) => (v === undefined || v === null ? '–' : Number(v).toFixed(1))
  const t = (ms) => new Date(ms).toLocaleTimeString('de-DE')
  $: last = readings.length ? readings[readings.length - 1] : null
  $: bad = readings.filter((r) => r.status !== 'good' || r.nonCorrDelta > 0).length
  $: nonCorr = readings.reduce((a, r) => a + (r.nonCorrDelta || 0), 0)
</script>

{#if last}
  <div class="head">
    <span class="badge s-{last.status}">{ST[last.status]}</span>
    <span>Letzte Abfrage {t(last.t)} ({last.reason})</span>
    <span class="muted">· {readings.length} Abfragen, {bad} auffällig, {nonCorr} neue nicht korrigierbare Fehler</span>
  </div>
  <table class="sum">
    <tr><th>Downstream</th><td>{last.ds.length} Kanäle · Pegel {f1(last.dsPowerMin)} … {f1(last.dsPowerMax)} dBmV · SNR/MER min {f1(last.snrMin)} dB</td></tr>
    <tr><th>Upstream</th><td>{last.us.length} Kanäle · Sendepegel {f1(last.usPowerMin)} … {f1(last.usPowerMax)} dBmV</td></tr>
    <tr><th>Fehler (Zähler der Box)</th><td>korrigierbar {last.corr} · nicht korrigierbar {last.nonCorr} (seit letzter Abfrage +{last.corrDelta} / +{last.nonCorrDelta})</td></tr>
  </table>
  {#if last.issues?.length}
    <ul class="issues">{#each last.issues as i}<li>{i}</li>{/each}</ul>
  {/if}
  <details>
    <summary>Kanäle der letzten Abfrage</summary>
    <div class="scroll">
      <table class="ch">
        <tr><th>Richtung</th><th>Kanal</th><th>DOCSIS</th><th>Frequenz MHz</th><th>Modulation</th><th>Pegel dBmV</th><th>SNR/MER dB</th><th>korr.</th><th>nicht korr.</th><th>Bewertung</th></tr>
        {#each [...last.ds.map((c) => ['DS', c]), ...last.us.map((c) => ['US', c])] as [dir, c]}
          <tr class="s-{c.status}"><td>{dir}</td><td>{c.id}</td><td>{c.version}</td><td>{c.frequency}</td><td>{c.modulation}</td><td>{f1(c.power)}</td>
            <td>{dir === 'DS' ? f1(c.snr) : ''}</td><td>{dir === 'DS' ? c.corr : ''}</td><td>{dir === 'DS' ? c.nonCorr : ''}</td><td title={c.issue || ''}>{ST[c.status]}</td></tr>
        {/each}
      </table>
    </div>
  </details>
  {#if !live || readings.length > 1}
    <details>
      <summary>Verlauf aller Abfragen</summary>
      <div class="scroll">
        <table class="ch">
          <tr><th>Zeit</th><th>Anlass</th><th>Status</th><th>SNR/MER min</th><th>DS-Pegel</th><th>US-Pegel</th><th>korr. Δ</th><th>nicht korr. Δ</th></tr>
          {#each readings.slice().reverse() as r}
            <tr class="s-{r.status}"><td>{new Date(r.t).toLocaleString('de-DE')}</td><td>{r.reason}</td><td>{ST[r.status]}</td><td>{f1(r.snrMin)}</td>
              <td>{f1(r.dsPowerMin)} … {f1(r.dsPowerMax)}</td><td>{f1(r.usPowerMin)} … {f1(r.usPowerMax)}</td><td>{r.corrDelta}</td><td>{r.nonCorrDelta}</td></tr>
          {/each}
        </table>
      </div>
    </details>
  {/if}
{:else}
  <p class="muted">{live ? 'Noch keine DOCSIS-Abfrage. Sie erfolgt beim Messbeginn, im eingestellten Intervall und bei jeder Störung.' : 'Für diese Messung liegen keine DOCSIS-Werte vor.'}</p>
{/if}

<style>
  .head { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; font-size: 13px; margin-bottom: 6px; }
  .badge { padding: 1px 8px; border-radius: 8px; color: #fff; font-size: 12px; }
  .badge.s-good { background: #2a9d8f; }
  .badge.s-warning { background: #e9a23b; }
  .badge.s-critical { background: #d62828; }
  .muted { color: var(--muted); font-size: 12px; }
  .sum th { text-align: left; color: var(--muted); width: 20%; }
  .sum td { text-align: left; }
  .issues { font-size: 12px; color: var(--bad); margin: 6px 0; }
  details { margin: 6px 0; }
  summary { cursor: pointer; font-size: 13px; font-weight: 600; }
  .scroll { overflow: auto; max-height: 360px; }
  .ch td, .ch th { font-size: 12px; white-space: nowrap; }
  tr.s-warning td { background: var(--warn-bg); }
  tr.s-critical td { background: var(--crit-bg); }
</style>
