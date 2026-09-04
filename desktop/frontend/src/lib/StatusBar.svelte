<script>
  import {onMount} from 'svelte'

  let {
    statusMsg = 'Ready',
    gameCount = 0,
    pipelineBusy = false,
    pipelineLabel = '',
    appUpdateDownloading = false,
    // P2 item 11 (docs/desktop-ui-research.md §7): the footer used to be
    // just message + pulse + a raw monospace count — "console real estate".
    // These two give it something worth looking at: when the library was
    // last synced, and a nudge when a newer app version exists.
    lastSyncAt = '',       // ISO timestamp or '' (never synced this session/install)
    updateAvailable = null, // UpdateInfo-shaped ({latestVersion, ...}) or null
    onGoToUpdate = () => {},
  } = $props()

  // Ticks once a minute so "Xm ago" keeps advancing without a reload — the
  // one status-bar value that goes stale just sitting on screen.
  let now = $state(Date.now())
  onMount(() => {
    const id = setInterval(() => { now = Date.now() }, 60000)
    return () => clearInterval(id)
  })

  function relativeSync(iso) {
    if (!iso) return null
    const mins = Math.floor((now - new Date(iso).getTime()) / 60000)
    if (mins < 1) return 'just now'
    if (mins < 60) return `${mins}m ago`
    const hours = Math.floor(mins / 60)
    if (hours < 24) return `${hours}h ago`
    return `${Math.floor(hours / 24)}d ago`
  }

  let syncLabel = $derived(relativeSync(lastSyncAt))
</script>

<footer class="status-bar">
  <span class="status-msg">{statusMsg}</span>
  <span class="status-right">
    {#if updateAvailable}
      <button
        class="status-update"
        onclick={onGoToUpdate}
        title="{updateAvailable.latestVersion || 'A new version'} is available — see Settings"
      >
        ⇪ Update available
      </button>
    {/if}
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
    {#if syncLabel}
      <span class="status-sync" title="Last full library sync">Synced {syncLabel}</span>
    {/if}
    <span class="status-count">{gameCount} game{gameCount !== 1 ? 's' : ''}</span>
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
    gap: 12px;
    flex-shrink: 0;
  }

  .status-update {
    background: transparent;
    border: none;
    padding: 0;
    color: var(--accent);
    font-size: 11px;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .status-update:hover { text-decoration: underline; }

  .status-pipeline {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: var(--accent);
    max-width: 260px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex-shrink: 0;
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

  .status-sync {
    white-space: nowrap;
    flex-shrink: 0;
  }

  .status-count {
    font-family: var(--font-mono);
    white-space: nowrap;
    flex-shrink: 0;
  }
</style>
