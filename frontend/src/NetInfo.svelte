<script>
  export let net = null // netinfo.Snapshot of the session
  const j = (xs) => (xs && xs.length ? xs.join(', ') : '–')
</script>

{#if net}
  <table class="net">
    <tr><th>Rechner</th><td>{net.hostname} ({net.os})</td><th>Lokale IP</th><td>{net.prefix || net.localIp || '–'}</td></tr>
    <tr><th>Netzwerkadapter</th><td>{net.interface || '–'}</td><th>MAC / MTU</th><td>{net.mac || '–'} / {net.mtu || '–'}</td></tr>
    <tr><th>Standard-Gateway</th><td>{j(net.gateways)}</td><th>DNS-Server</th><td>{j(net.dns)}</td></tr>
  </table>
  <details><summary>Routing-Tabelle</summary><pre>{net.routes}</pre></details>
  <details><summary>ARP-Tabelle</summary><pre>{net.arp}</pre></details>
  <details><summary>Adapter-Konfiguration</summary><pre>{net.ipconfig}</pre></details>
  <p class="muted">Stand: {new Date(net.takenAt).toLocaleString('de-DE')} (Beginn der Messung)</p>
{:else}
  <p class="muted">Für diese Messung liegen keine Netzwerk-Infos vor (erst ab v0.17.0 aufgezeichnet).</p>
{/if}

<style>
  .net th { text-align: left; color: var(--muted); font-weight: 600; width: 16%; }
  .net td { text-align: left; }
  details { margin: 6px 0; }
  summary { cursor: pointer; font-size: 13px; font-weight: 600; }
  pre { background: var(--neutral-bg); padding: 8px 10px; font-size: 11px; overflow: auto; max-height: 320px; border-radius: 6px; margin: 6px 0 0; }
  .muted { color: var(--muted); font-size: 12px; }
</style>
