<script>
  export let incidents = []
  export let onSelect = (inc) => {} // e.g. zoom the chart to the incident

  const CLASS = {
    lan: 'Heimnetz', isp: 'Anbieter-Zugang', isp_core: 'Anbieter-Netz/Internet',
    target: 'nur Hauptziel', alt: 'nur Ausweichziel', device: 'nur Einzel-Messpunkt',
  }
  const STATE = { '.': 'antwortet', s: 'langsam', x: 'keine Antwort', '-': 'nicht gemessen' }
  const CELL = { '.': 'ok', s: 'slow', x: 'lost', '-': 'none' }
  const t = (ms) => new Date(ms).toLocaleTimeString('de-DE')
  const dt = (ms) => new Date(ms).toLocaleString('de-DE')

  let open = new Set()
  let filter = ''
  $: shown = incidents.filter((i) => !filter || i.class === filter).slice().reverse()
  $: if (shown.length && open.size === 0) open = new Set([shown[0].id])
  $: counts = incidents.reduce((m, i) => ((m[i.class] = (m[i.class] || 0) + 1), m), {})

  function toggle(id) {
    open.has(id) ? open.delete(id) : open.add(id)
    open = new Set(open)
  }
</script>

<div class="bar">
  <select bind:value={filter}>
    <option value="">Alle Störungen ({incidents.length})</option>
    {#each Object.keys(CLASS) as c}{#if counts[c]}<option value={c}>{CLASS[c]} ({counts[c]})</option>{/if}{/each}
  </select>
  <span class="legend">
    <i class="cell ok"></i> antwortet <i class="cell slow"></i> langsam <i class="cell lost"></i> keine Antwort
    <i class="cell none"></i> nicht gemessen · blasse Spalten = 10 s davor / 5 s danach
  </span>
</div>

{#each shown as inc (inc.id)}
  <div class="inc c-{inc.class}">
    <button class="head" on:click={() => toggle(inc.id)}>
      <span class="badge b-{inc.class}">{CLASS[inc.class] || inc.class}</span>
      <b>{dt(inc.t)}</b> · {inc.seconds} s · {inc.title}
      <span class="chev">{open.has(inc.id) ? '▾' : '▸'}</span>
    </button>
    {#if open.has(inc.id)}
      <p class="detail">{inc.detail} <button class="link" on:click={() => onSelect(inc)}>im Diagramm zeigen</button></p>
      <div class="mx">
        <table>
          <tr>
            <td></td>
            {#each inc.columns as c, i}
              <td class="tick" class:pre={i < inc.preRoll || i >= inc.columns.length - inc.postRoll}>{i % 5 === 0 ? t(c).slice(3) : ''}</td>
            {/each}
          </tr>
          {#each inc.series as s}
            <tr>
              <td class="lbl" class:target={s.target} class:custom={s.custom}>{s.label}{#if s.lost} <span class="lostn">{s.lost} s weg</span>{/if}</td>
              {#each s.states.split('') as st, i}
                <td class="cell {CELL[st]}" class:pre={i < inc.preRoll || i >= inc.columns.length - inc.postRoll}
                  title="{t(inc.columns[i])} · {STATE[st]}{s.rtt[i] >= 0 ? ' · ' + s.rtt[i].toFixed(1) + ' ms' : ''}"></td>
              {/each}
            </tr>
          {/each}
        </table>
      </div>
    {/if}
  </div>
{:else}
  <p class="muted">Keine Störungen.</p>
{/each}

<style>
  .bar { display: flex; gap: 14px; align-items: center; flex-wrap: wrap; margin-bottom: 8px; font-size: 12px; }
  .legend { color: var(--muted); }
  .inc { border: 1px solid var(--border); border-left: 5px solid #d62828; border-radius: 6px; margin-bottom: 8px; }
  .c-target, .c-alt { border-left-color: #3a5a8c; }
  .c-lan, .c-device { border-left-color: #2a9d8f; }
  .head { display: flex; gap: 8px; align-items: center; width: 100%; text-align: left; border: none; background: none; padding: 8px 10px; font-size: 13px; }
  .chev { margin-left: auto; color: var(--muted); }
  .detail { margin: 0 10px 8px; font-size: 12px; color: var(--muted); }
  .link { border: none; background: none; color: var(--accent); padding: 0; text-decoration: underline; font-size: 12px; }
  .mx { overflow-x: auto; padding: 0 10px 10px; }
  table { border-collapse: separate; border-spacing: 1px; width: auto; }
  td { padding: 0; border: none; }
  td.lbl { padding-right: 10px; white-space: nowrap; text-align: left; font-size: 12px; }
  td.lbl.target { font-weight: 600; }
  td.lbl.custom { font-style: italic; }
  .lostn { color: #d62828; font-style: normal; font-size: 11px; margin-left: 6px; }
  td.tick { font-size: 9px; color: var(--muted); white-space: nowrap; height: 12px; }
  .cell, i.cell { width: 9px; min-width: 9px; height: 14px; border-radius: 2px; display: table-cell; }
  i.cell { display: inline-block; width: 10px; height: 10px; vertical-align: middle; }
  .ok { background: #52b788; }
  .slow { background: #f4a261; }
  .lost { background: #d62828; }
  .none { background: #ddd; }
  .pre { opacity: 0.4; }
  .badge { font-size: 11px; padding: 1px 7px; border-radius: 8px; color: #fff; background: #d62828; white-space: nowrap; }
  .b-target, .b-alt { background: #3a5a8c; }
  .b-lan, .b-device { background: #2a9d8f; }
  .muted { color: var(--muted); }
</style>
