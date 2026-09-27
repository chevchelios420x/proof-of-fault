<script>
  import Help from './Help.svelte'
  export let settings // current settings (object)
  export let onSave = async (s) => {}
  export let onClose = () => {}
  export let onDefaults = async () => settings
  export let onPreviewTheme = (t) => {}

  let s = JSON.parse(JSON.stringify(settings))
  let saving = false
  let err = ''
  const ZONES = [
    ['LAN', 'LAN – Heimnetz'],
    ['ISP_EDGE', 'ISP_EDGE – Anbieter-Zugang'],
    ['WAN', 'WAN – Internet/Ziele'],
  ]

  async function save() {
    saving = true
    err = ''
    try { await onSave(s); onClose() } catch (e) { err = String(e) }
    saving = false
  }
  async function defaults() {
    const d = await onDefaults()
    s = { ...JSON.parse(JSON.stringify(d)), lastTarget: s.lastTarget }
    onPreviewTheme(s.theme)
  }
  function cancel() {
    onPreviewTheme(settings.theme)
    onClose()
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="backdrop" on:click|self={cancel}>
  <div class="modal" role="dialog" aria-label="Einstellungen">
    <div class="top">
      <h2>⚙ Einstellungen</h2>
      <button class="x" on:click={cancel} title="Schließen">✕</button>
    </div>

    <div class="body">
      <section>
        <h3>Darstellung</h3>
        <label class="row">Farbschema
          <select bind:value={s.theme} on:change={() => onPreviewTheme(s.theme)}>
            <option value="auto">automatisch (wie Windows)</option>
            <option value="light">hell</option>
            <option value="dark">dunkel</option>
          </select>
        </label>
        <label class="row">Zeitraum des Live-Diagramms beim Start
          <select bind:value={s.defaultWindowMin}>
            <option value={5}>5 min</option><option value={30}>30 min</option><option value={60}>60 min</option>
            <option value={90}>90 min</option><option value={0}>gesamte Messung</option>
          </select>
        </label>
      </section>

      <section>
        <h3>Latenzspitzen je Zone
          <Help align="left" text={'Eine Messung gilt als Latenzspitze, wenn sie\n• mehr als „Faktor“ × so lang dauert wie üblich (Median der letzten Minute) UND mindestens „Mindestabstand“ ms darüber liegt,\n• ODER die feste Grenze überschreitet (0 = aus).\nBeispiel WAN: üblich 15 ms, Faktor 2, Mindestabstand 20 ms → ab 35 ms eine Spitze.'} />
        </h3>
        <table class="grid">
          <tr><th></th><th>Faktor ×<Help text="Wievielfach langsamer als üblich (z. B. 2 = doppelt so lang)." /></th><th>Mindestabstand ms<Help text="So viele ms muss die Messung mindestens über dem üblichen Wert liegen – verhindert Fehlalarme bei sehr kleinen Werten (z. B. 1 → 3 ms)." /></th><th>feste Grenze ms<Help align="right" text="Jede Messung darüber zählt immer als Spitze, egal wie der übliche Wert ist. 0 = aus." /></th></tr>
          {#each ZONES as [z, label]}
            <tr>
              <td>{label}</td>
              <td><input type="number" min="1" step="0.5" bind:value={s.spikes[z].factor} /></td>
              <td><input type="number" min="0" step="1" bind:value={s.spikes[z].minDeltaMs} /></td>
              <td><input type="number" min="0" step="10" bind:value={s.spikes[z].absoluteMs} /></td>
            </tr>
          {/each}
        </table>
        <p class="note">Manuelle Messpunkte verwenden die Werte ihrer Zone („keine“ → WAN-Werte).</p>
      </section>

      <section>
        <h3>Ausfälle &amp; Störungen</h3>
        <label class="row">Ausfall ab … Sekunden ohne Antwort in Folge
          <span><input type="number" min="1" max="600" bind:value={s.outageAfter} /><Help align="right" text="Ab wie vielen aufeinanderfolgenden Sekunden ohne Antwort (bis zum Ziel) ein „Ausfall“ gemeldet wird. Kürzere Aussetzer erscheinen trotzdem als Störung/Verlust." /></span>
        </label>
        <label class="row">Wartezeit auf Antwort (Timeout, Sekunden)
          <span><input type="number" min="0.5" max="10" step="0.5" bind:value={s.probeTimeoutSec} /><Help align="right" text="Kommt innerhalb dieser Zeit keine Antwort, zählt der Ping als verloren. Kleiner = strenger (sehr langsame Antworten zählen als Verlust)." /></span>
        </label>
        <label class="row">Störung: Sekunden davor anzeigen
          <span><input type="number" min="0" max="120" bind:value={s.incidentPreSec} /><Help align="right" text="Wie viele Sekunden vor Beginn einer Störung in der Matrix zum Vergleich gezeigt werden." /></span>
        </label>
        <label class="row">Störung endet nach … Sekunden ohne Probleme
          <span><input type="number" min="1" max="120" bind:value={s.incidentPostSec} /><Help align="right" text="Erst wenn so viele Sekunden in Folge alles in Ordnung war, gilt eine Störung als beendet. Kurz aufeinanderfolgende Aussetzer werden so zu einer Störung zusammengefasst." /></span>
        </label>
        <label class="row">Route prüfen alle … Minuten
          <span><input type="number" min="1" max="1440" bind:value={s.rediscoverMin} /><Help align="right" text="Wie oft der Weg zum Ziel (Traceroute) neu ermittelt wird. Bei Ausfällen wird zusätzlich sofort geprüft. Gilt ab der nächsten Messung." /></span>
        </label>
        <label class="row">„Hohe Latenz“ im Bericht ab … ms
          <span><input type="number" min="1" step="10" bind:value={s.highLatencyMs} /><Help align="right" text="Für die Spalte „> X ms“ in den Kennzahlen: Anzahl der Messungen über diesem Wert." /></span>
        </label>
      </section>

      <section>
        <h3>Komfort</h3>
        <label class="check"><input type="checkbox" bind:checked={s.preventSleep} /> Standby verhindern, solange gemessen wird
          <Help align="left" text="Hält den PC während einer Messung wach (der Bildschirm darf trotzdem ausgehen). Sonst unterbricht der Energiesparmodus die Messung und es entstehen Lücken im Nachweis." /></label>
        <label class="check"><input type="checkbox" bind:checked={s.autoStart} /> Beim Start der App die letzte Messung automatisch fortsetzen
          <Help align="left" text={'Startet beim Öffnen der App sofort eine neue Messung zum zuletzt verwendeten Ziel' + (s.lastTarget ? ' (' + s.lastTarget + ')' : '') + '. Praktisch zusammen mit einer Verknüpfung im Autostart-Ordner von Windows.'} /></label>
        <label class="row">Alte Messungen automatisch löschen nach … Tagen
          <span><input type="number" min="0" bind:value={s.retentionDays} /><Help align="right" text="Beendete Messungen, die älter sind, werden beim Start der App gelöscht, damit die Datenbank nicht unbegrenzt wächst. 0 = nie löschen." /></span>
        </label>
      </section>
      {#if err}<p class="err">{err}</p>{/if}
    </div>

    <div class="actions">
      <button on:click={defaults}>Standardwerte</button>
      <span class="grow"></span>
      <button on:click={cancel}>Abbrechen</button>
      <button class="primary" disabled={saving} on:click={save}>Speichern</button>
    </div>
  </div>
</div>

<style>
  .backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.45); display: flex; align-items: center; justify-content: center; z-index: 500; }
  .modal { background: var(--card); color: var(--fg); border: 1px solid var(--border); border-radius: 12px; width: min(760px, 94vw); max-height: 90vh; display: flex; flex-direction: column; box-shadow: 0 10px 40px rgba(0, 0, 0, 0.35); }
  .top { display: flex; justify-content: space-between; align-items: center; padding: 14px 18px 6px; }
  .top h2 { margin: 0; font-size: 17px; }
  .x { border: none; background: none; font-size: 16px; color: var(--muted); }
  .body { overflow: auto; padding: 0 18px 10px; }
  section { border-top: 1px solid var(--border); padding: 10px 0; }
  h3 { font-size: 13px; margin: 0 0 8px; color: var(--fg); }
  .row { display: flex; justify-content: space-between; align-items: center; gap: 12px; font-size: 13px; margin: 6px 0; }
  .row input { width: 90px; }
  .check { display: flex; align-items: center; gap: 8px; font-size: 13px; margin: 6px 0; }
  .grid input { width: 90px; }
  .grid td, .grid th { border: none; padding: 3px 6px; }
  .note { font-size: 12px; color: var(--muted); margin: 6px 0 0; }
  .actions { display: flex; gap: 8px; padding: 12px 18px; border-top: 1px solid var(--border); }
  .grow { flex: 1; }
  .err { color: var(--bad); }
</style>
