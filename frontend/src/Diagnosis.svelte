<script>
  export let d = null
  export let live = false
  import Help from './Help.svelte'
  import { HELP } from './help.js'
</script>

{#if d}
  <div class="diag lvl-{d.level}">
    <div class="head">
      <h2>{live ? 'Zwischenstand: ' : 'Diagnose: '}{d.headline}</h2>
      <span class="conf">Sicherheit: <b>{d.confidence}</b><Help align="right" text={HELP.diagnosis} /></span>
    </div>
    <ul>{#each d.explanation || [] as x}<li>{x}</li>{/each}</ul>
    <p class="adv"><b>Was tun?</b></p>
    <ul>{#each d.advice || [] as x}<li>{x}</li>{/each}</ul>
  </div>
{/if}

<style>
  .diag { border-left: 6px solid #e76f51; background: var(--warn-bg); padding: 10px 16px; border-radius: 8px; }
  .lvl-none { border-color: #2a9d8f; background: var(--ok-bg); }
  .lvl-lan { border-color: #2a9d8f; }
  .lvl-wan { border-color: #3a5a8c; background: var(--info-bg); }
  .lvl-unclear { border-color: #999; background: var(--neutral-bg); }
  .head { display: flex; justify-content: space-between; gap: 12px; align-items: baseline; flex-wrap: wrap; }
  h2 { font-size: 16px; margin: 0; }
  .conf { font-size: 12px; white-space: nowrap; }
  ul { margin: 6px 0; padding-left: 20px; font-size: 13px; }
  .adv { margin: 6px 0 0; font-size: 13px; }
</style>
