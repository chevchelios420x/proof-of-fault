<script>
  import Help from './Help.svelte'
  import { play } from './sound.js'
  export let settings // current settings (object)
  export let onSave = async (s) => {}
  export let onClose = () => {}
  export let onDefaults = async () => settings
  export let onPreviewTheme = (t) => {}
  export let onTest = async (url, user, password) => ({})
  export let onDebug = async (url, user, password) => ''

  let debugging = false
  let debugRes = ''
  async function debugDump() {
    if (!confirm('Debug-Daten der FRITZ!Box sammeln?\n\nDie App meldet sich an und liest alle bekannten Status-Seiten einmalig aus (nur lesend, an der Box wird nichts verändert). Das dauert ca. 10 Sekunden.\n\nDie ZIP-Datei kann persönliche Daten enthalten (Gerätenamen, MAC-/IP-Adressen, Telefonnummern aus dem Ereignisprotokoll). Session-ID und Kennwort-Felder werden entfernt. Nur an Personen weitergeben, denen du vertraust – nicht öffentlich posten.')) return
    debugging = true
    debugRes = ''
    try {
      const p = await onDebug(s.fritz.url, s.fritz.user, s.fritz.password || '')
      debugRes = p ? '✓ gespeichert: ' + p : ''
    } catch (e) { debugRes = '✗ ' + e }
    debugging = false
  }

  let testing = false
  let testRes = null
  async function test() {
    testing = true
    testRes = null
    const key = accessKey
    try { testRes = await onTest(s.fritz.url, s.fritz.user, s.fritz.password || '', s.access) } catch (e) { testRes = { error: String(e) } }
    testedKey = key
    testing = false
  }

  let s = JSON.parse(JSON.stringify(settings))
  // Changed FRITZ!Box access (address, user, password) should be tested.
  $: accessKey = [s.fritz.url, s.fritz.user, s.fritz.password || '', s.fritz.hasPassword].join('\u0001')
  const origKey = [settings.fritz?.url, settings.fritz?.user, '', settings.fritz?.hasPassword].join('\u0001')
  let testedKey = origKey
  $: untested = accessKey !== testedKey && !(s.fritz.password === '' && !s.fritz.hasPassword)
  let saving = false
  let err = ''
  const ZONES = [
    ['LAN', 'LAN – Heimnetz'],
    ['ISP_EDGE', 'ISP_EDGE – Anbieter-Zugang'],
    ['WAN', 'WAN – Internet/Ziele'],
  ]

  const ROLES = [
    ['LAN', 'wie LAN (Heimnetz)'],
    ['ISP_EDGE', 'wie ISP_EDGE (Anbieter)'],
    ['WAN', 'wie WAN (Ziel/Ausweichziel)'],
    ['none', 'nicht werten'],
  ]
  function addZone() {
    s.zones = [...s.zones, { id: 'z' + Date.now().toString(36), name: 'Neue Zone', color: '#8e44ad', role: 'none', description: '', builtin: false }]
  }
  function removeZone(id) {
    s.zones = s.zones.filter((z) => z.id !== id)
  }

  async function save() {
    if (untested && confirm('Die FRITZ!Box-Zugangsdaten wurden geändert, aber noch nicht getestet.\n\nOK = jetzt Verbindung testen\nAbbrechen = ohne Test speichern')) {
      await test()
      return
    }
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
        <h3>Anschluss &amp; FRITZ!Box
          <Help align="left" text={'Bei Kabelanschluss (DOCSIS) liest die App die Leitungswerte der FRITZ!Box: Downstream-/Upstream-Pegel, SNR/MER und die Zähler für korrigierbare und nicht korrigierbare Fehler. Dafür braucht sie einen FRITZ!Box-Benutzer (am besten ein eigener Benutzer mit dem Recht „FRITZ!Box Einstellungen“). Das Kennwort wird mit Windows-Datenschutz (DPAPI) verschlüsselt gespeichert und nie angezeigt. DSL- und Glasfaser-Auswertung sind vorbereitet.'} />
        </h3>
        <label class="row">Anschlussart
          <select bind:value={s.access}>
            <option value="dsl">DSL (vorbereitet)</option>
            <option value="docsis">Kabel (DOCSIS)</option>
            <option value="fibre">Glasfaser (vorbereitet)</option>
            <option value="mobile">LTE/5G (vorbereitet)</option>
          </select>
        </label>
        <label class="row">FRITZ!Box-Adresse (IP oder Name)
          <input class="wide" bind:value={s.fritz.url} placeholder="z. B. 192.168.178.1 oder fritz.box" />
        </label>
        <label class="row">Benutzername
          <input class="wide" bind:value={s.fritz.user} placeholder="leer = zuletzt angemeldeter Benutzer" />
        </label>
        <label class="row">Kennwort
          <input class="wide" type="password" bind:value={s.fritz.password} placeholder={s.fritz.hasPassword ? '•••••• gespeichert – neues eingeben zum Ändern' : 'Kennwort'} autocomplete="off" />
        </label>
        {#if s.fritz.hasPassword}
          <label class="check"><input type="checkbox" checked={!s.fritz.hasPassword} on:change={(e) => (s.fritz.hasPassword = !e.target.checked)} /> gespeichertes Kennwort löschen</label>
        {/if}
        <label class="row">DOCSIS-Werte abfragen alle … Sekunden
          <span><input type="number" min="15" max="3600" step="5" bind:value={s.fritz.intervalSec} /><Help align="right" text="Regelmäßige Abfrage zusätzlich zu den Abfragen bei Beginn und Ende jeder Störung. 60 s ist ein guter Wert; die FRITZ!Box verkraftet auch 15–30 s, rechnet die Seite aber jedes Mal neu. Minimum 15 s." /></span>
        </label>
        {#if debugRes}<p class="small {debugRes.startsWith('✓') ? 'ok' : 'err'}">{debugRes}</p>{/if}
        {#if untested && !testing}
          <div class="hint">Zugangsdaten geändert – bitte einmal <b>Verbindung testen</b>, damit die Messung später nicht an der Anmeldung scheitert.</div>
        {/if}
        <div class="row">
          <span class="btns">
            <button class:primary={untested} on:click={test} disabled={testing}>{testing ? 'Teste …' : 'Verbindung testen'}</button>
            <button on:click={debugDump} disabled={debugging}>{debugging ? 'Sammle …' : '🐞 Debug'}</button>
            <Help align="right" text={'Sammelt einmalig alle bekannten Status-Seiten der FRITZ!Box (nur lesend) und speichert sie als ZIP – als Hilfe für die Entwicklung, z. B. für DSL-, Glasfaser- oder LTE/5G-Auswertung.\n\n⚠ Nicht weitergeben bzw. nur an vertraute Personen: Die Datei kann Gerätenamen, MAC-/IP-Adressen, Telefonnummern (Ereignisprotokoll) und Anschlussdaten enthalten. Session-ID und Kennwort-Felder werden automatisch entfernt.'} />
          </span>
          {#if testRes}
            {#if testRes.ok}
              {#if testRes.status}
                <span class="ok">✓ {testRes.model || 'FRITZ!Box'}: {testRes.ds} Downstream- / {testRes.us} Upstream-Kanäle, Leitungswerte {({ good: 'in Ordnung', warning: 'auffällig', critical: 'kritisch' })[testRes.status]}</span>
              {:else}
                <span class="ok">✓ {testRes.model || 'FRITZ!Box'}: Anmeldung erfolgreich (Leitungswerte für diese Anschlussart folgen in einer späteren Version)</span>
              {/if}
            {:else}
              <span class="err">✗ {testRes.model ? testRes.model + ': ' : ''}{testRes.error}</span>
            {/if}
          {/if}
        </div>
      </section>

      <section>
        <h3>Zonen
          <Help align="left" text={'Die Bereiche, in die Hops und manuelle Messpunkte eingeteilt werden. Die vier Standardzonen lassen sich umbenennen und umfärben. Eigene Zonen (z. B. „VPN“, „Server“, „Kunde A“) dienen der Gruppierung in Diagramm, Störungs-Matrix und Bericht; „Auswertung“ legt fest, nach welchen Regeln sie zählen:\n• wie LAN / ISP_EDGE: gehört zu diesem Abschnitt des Weges\n• wie WAN: Ausweichziel\n• nicht werten: nur anzeigen'} />
        </h3>
        <table class="grid zones">
          <tr><th>Farbe</th><th>Name</th><th>Auswertung</th><th>Beschreibung</th><th></th></tr>
          {#each s.zones as z (z.id)}
            <tr>
              <td><input type="color" bind:value={z.color} class="color" /></td>
              <td><input bind:value={z.name} class="zname" /></td>
              <td>
                {#if z.builtin}<span class="muted">Standard ({z.id})</span>
                {:else}
                  <select bind:value={z.role}>{#each ROLES as [r, l]}<option value={r}>{l}</option>{/each}</select>
                {/if}
              </td>
              <td><input bind:value={z.description} class="zdesc" placeholder="optional" /></td>
              <td>{#if !z.builtin}<button class="del" title="Zone löschen" on:click={() => removeZone(z.id)}>✕</button>{/if}</td>
            </tr>
          {/each}
        </table>
        <button on:click={addZone}>+ Zone hinzufügen</button>
        <p class="note">Messpunkte einer gelöschten Zone werden nicht mehr gewertet, bis sie einer anderen Zone zugeordnet sind. Hops der Route verwenden die drei Standardzonen.</p>
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
        <p class="note">Manuelle Messpunkte verwenden die Werte ihrer Zone bzw. der Zone, wie die sie gewertet wird („nicht werten“ → WAN-Werte).</p>
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
        <h3>Signaltöne
          <Help align="left" text={'Spielt einen Ton, wenn ein Ereignis eintritt – praktisch, wenn die App im Hintergrund läuft. Jede Art hat einen eigenen Klang (mit ▶ anhören). Verlust- und Spitzen-Töne kommen, sobald das Ereignis abgeschlossen ist (wenige Sekunden danach), Ausfall- und Routenwechsel-Töne sofort.'} />
        </h3>
        <label class="check"><input type="checkbox" bind:checked={s.sounds.enabled} /> Signaltöne aktivieren</label>
        <div class:disabled={!s.sounds.enabled}>
          <label class="check"><input type="checkbox" bind:checked={s.sounds.routeChange} /> bei Routenwechsel
            <button class="play" title="anhören" on:click|preventDefault={() => play('route', s.sounds.volume)}>▶</button></label>
          <table class="grid">
            <tr>
              <th></th>
              <th>Latenzspitze <button class="play" title="anhören" on:click={() => play('spike', s.sounds.volume)}>▶</button></th>
              <th>Paketverlust <button class="play" title="anhören" on:click={() => play('loss', s.sounds.volume)}>▶</button></th>
              <th>Ausfall <button class="play" title="anhören" on:click={() => play('outage', s.sounds.volume)}>▶</button></th>
            </tr>
            {#each ZONES as [z, label]}
              <tr>
                <td>{label}</td>
                <td class="c"><input type="checkbox" bind:checked={s.sounds.zones[z].spike} /></td>
                <td class="c"><input type="checkbox" bind:checked={s.sounds.zones[z].loss} /></td>
                <td class="c"><input type="checkbox" bind:checked={s.sounds.zones[z].outage} /></td>
              </tr>
            {/each}
          </table>
          <label class="row">Lautstärke
            <span><input type="range" min="0" max="100" bind:value={s.sounds.volume} class="range" /> {s.sounds.volume} %</span>
          </label>
          <label class="row">Mindestens … Sekunden zwischen gleichen Tönen
            <span><input type="number" min="0" max="3600" bind:value={s.sounds.cooldownSec} /><Help align="right" text="Verhindert Dauergepiepe bei vielen Ereignissen hintereinander: derselbe Ton für dieselbe Zone kommt höchstens einmal in diesem Zeitraum." /></span>
          </label>
          <p class="note">Die Zone ist die, der das Ereignis zugeordnet wurde (z. B. „Paketverlust ab Hop 3 [ISP_EDGE]“ → ISP_EDGE). Ausfälle manueller Messpunkte zählen als Paketverlust ihrer Zone.</p>
        </div>
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
  .modal { background: var(--card); color: var(--fg); border: 1px solid var(--border); border-radius: 12px; width: min(880px, 96vw); max-height: 90vh; display: flex; flex-direction: column; box-shadow: 0 10px 40px rgba(0, 0, 0, 0.35); }
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
  .ok { color: #2a9d8f; font-size: 12px; }
  .btns { display: inline-flex; gap: 6px; align-items: center; }
  .small { font-size: 12px; margin: 4px 0; word-break: break-all; }
  .hint { background: var(--warn-bg); border-left: 4px solid #e9a23b; padding: 6px 10px; font-size: 12px; border-radius: 4px; margin: 6px 0; }
  .row input.wide { width: 300px; }
  .disabled { opacity: 0.45; pointer-events: none; }
  .play { padding: 0 6px; font-size: 10px; margin-left: 4px; }
  .grid td.c { text-align: center; }
  .zones input.zname { width: 160px; }
  .zones input.zdesc { width: 200px; }
  .zones select { width: 200px; }
  .zones input.color { width: 40px; height: 28px; padding: 0 2px; }
  .del { padding: 1px 8px; font-size: 12px; }
  .muted { color: var(--muted); font-size: 12px; }
  .range { width: 160px; padding: 0; vertical-align: middle; }
</style>
