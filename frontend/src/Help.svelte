<script>
  // Small "?" badge that explains something on hover or keyboard focus.
  export let text = ''
  export let align = 'center' // center | left | right: where the bubble opens
</script>

<!-- svelte-ignore a11y-no-noninteractive-tabindex -->
<span class="help {align}" tabindex="0" role="note" aria-label={text} data-tip={text}>?</span>

<style>
  .help {
    display: inline-flex; align-items: center; justify-content: center;
    width: 15px; height: 15px; margin-left: 5px; border-radius: 50%;
    background: var(--help-bg); color: var(--help-fg); font-size: 10px; font-weight: 700; font-style: normal;
    cursor: help; position: relative; vertical-align: middle; line-height: 1; user-select: none;
  }
  .help:hover, .help:focus { background: #3a5a8c; color: #fff; outline: none; }
  .help:hover::after, .help:focus::after {
    content: attr(data-tip);
    position: absolute; top: 140%; z-index: 1000;
    width: max-content; max-width: 320px;
    background: #1f2a44; color: #fff; padding: 8px 10px; border-radius: 6px;
    font-size: 12px; font-weight: 400; line-height: 1.45; text-align: left; white-space: pre-line;
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.25);
  }
  .center:hover::after, .center:focus::after { left: 50%; transform: translateX(-50%); }
  .left:hover::after, .left:focus::after { left: 0; }
  .right:hover::after, .right:focus::after { right: 0; }
</style>
