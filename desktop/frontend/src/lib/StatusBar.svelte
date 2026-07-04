<script>
  let {statusMsg = 'Ready', gameCount = 0} = $props()
</script>

<footer class="status-bar">
  <span class="status-msg">{statusMsg}</span>
  <span class="status-right">
    {gameCount} game{gameCount !== 1 ? 's' : ''}
  </span>
</footer>

<style>
  .status-bar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    height: 24px;
    padding: 0 12px;
    background: var(--bg-tertiary);
    border-top: 1px solid var(--border);
    font-size: 11px;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .status-msg {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .status-right {
    flex-shrink: 0;
    font-family: var(--font-mono);
  }
</style>
