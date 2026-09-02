<script>
  let {
    statusMsg = 'Ready',
    gameCount = 0,
    pipelineBusy = false,
    pipelineLabel = '',
    appUpdateDownloading = false,
  } = $props()
</script>

<footer class="status-bar">
  <span class="status-msg">{statusMsg}</span>
  <span class="status-right">
    {#if appUpdateDownloading}
      <span class="status-pipeline" title="Runs in the background — it keeps going while you switch tabs">
        <span class="pipeline-dot"></span>
        Downloading app update…
      </span>
    {/if}
    {#if pipelineBusy}
      <span class="status-pipeline" title="Runs in the background — it keeps going while you switch tabs">
        <span class="pipeline-dot"></span>
        {pipelineLabel}
      </span>
    {/if}
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
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
    font-family: var(--font-mono);
  }

  .status-pipeline {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: var(--accent);
    max-width: 260px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .pipeline-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    flex-shrink: 0;
    animation: pipeline-pulse 1.2s ease-in-out infinite;
  }

  @keyframes pipeline-pulse {
    0%, 100% { opacity: 0.35; }
    50% { opacity: 1; }
  }
</style>