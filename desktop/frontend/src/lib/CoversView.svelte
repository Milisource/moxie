<script>
  import Button from './Button.svelte'

  // Presentational only — cover-fetch state and the covers:* event
  // subscriptions live in the app shell (App.svelte) so an in-flight run
  // survives tab switches, exactly like sync/scan/update state. This view
  // just renders the state and calls onFetch.
  let {
    gameCount = null,
    fetching = false,
    progress = {current: 0, total: 0, title: '', phase: ''},
    result = null,
    coverError = '',
    onFetch = () => {},
  } = $props()

  // ── Derived ─────────────────────────────────────────────────
  let progressPct = $derived.by(() => {
    if (!progress.total) return 0
    return Math.round((progress.current / progress.total) * 100)
  })

  let phaseLabel = $derived.by(() => {
    if (!progress.phase) return 'Starting…'
    if (progress.phase === 'resolving') {
      return `Finding covers… (${progress.current}/${progress.total})`
    }
    if (progress.phase === 'downloading') {
      return `Downloading covers… (${progress.current}/${progress.total})`
    }
    return 'Fetching covers…'
  })
</script>

<div class="covers-view">
  <div class="covers-header">
    <h2>Cover Art</h2>
    <p class="covers-subtitle">
      Download cover art for games that don't have one yet. Games with a stored
      cover URL download directly; the rest are looked up on F95Zone
      (cookie-free first, your session cookies as a fallback).
    </p>
    {#if gameCount !== null}
      <p class="text-muted">
        {gameCount} game{gameCount !== 1 ? 's' : ''} in the library — covers
        appear in the Library list and the game detail view.
      </p>
    {/if}
  </div>

  <!-- ── Fetch Button ───────────────────────────────────────── -->
  <div class="action-bar">
    <Button variant="primary" onclick={onFetch} disabled={fetching}>
      {fetching ? 'Fetching…' : 'Fetch Missing Covers'}
    </Button>
  </div>

  <!-- ── Progress ───────────────────────────────────────────── -->
  {#if fetching}
    <div class="progress-section">
      <div class="progress-bar-bg">
        <div class="progress-bar-fill" style="width: {progressPct}%"></div>
      </div>
      <p class="progress-label">{phaseLabel}</p>
      {#if progress.title}
        <p class="progress-current text-muted">{progress.title}</p>
      {/if}
    </div>
  {/if}

  <!-- ── Completion Result ──────────────────────────────────── -->
  {#if result}
    <div class="result-section">
      <div class="result-header">
        <span class="result-icon">✓</span>
        <div class="result-body">
          <p class="result-title">Cover Fetch Complete</p>
          <p class="result-summary">
            {#if result.total === 0}
              Nothing to do — every game already has a cover.
            {:else}
              {result.fetched} fetched, {result.failed} failed, {result.skipped} skipped
              ({result.total} missing before this run)
              {#if result.backfilled}
                — {result.backfilled} thumbnails backfilled
              {/if}
            {/if}
          </p>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── Error ──────────────────────────────────────────────── -->
  {#if coverError}
    <div class="error-section">
      <p class="error-title">Cover fetch failed:</p>
      <p class="error-line">{coverError}</p>
    </div>
  {/if}
</div>

<style>
  .text-muted { color: var(--text-muted); }
  .covers-view {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 24px;
    overflow-y: auto;
  }

  .covers-header h2 {
    margin: 0 0 6px;
    font-size: var(--text-2xl);
  }

  .covers-subtitle {
    margin: 0 0 8px;
    color: var(--text-muted);
    max-width: 640px;
    line-height: 1.5;
  }

  .action-bar {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .progress-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 8px;
  }

  .progress-bar-bg {
    height: 10px;
    background: var(--bg-secondary);
    border-radius: var(--radius-1);
    overflow: hidden;
  }

  .progress-bar-fill {
    height: 100%;
    background: var(--accent);
    border-radius: var(--radius-1);
    transition: width 0.2s ease;
  }

  .progress-label {
    margin: 0;
    font-size: var(--text-base);
  }

  .progress-current {
    margin: 0;
    font-size: var(--text-sm);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .result-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 14px 16px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
  }

  .result-header {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .result-icon {
    color: var(--success);
    font-size: var(--text-xl);
  }

  .result-body p {
    margin: 0;
  }

  .result-title {
    font-weight: 600;
  }

  .result-summary {
    color: var(--text-muted);
    font-size: var(--text-base);
  }

  .error-section {
    padding: 12px 16px;
    background: rgba(220, 60, 60, 0.08);
    border: 1px solid rgba(220, 60, 60, 0.4);
    border-radius: var(--radius-1);
  }

  .error-title {
    margin: 0 0 4px;
    font-weight: 600;
    color: var(--danger);
  }

  .error-line {
    margin: 0;
    font-size: var(--text-base);
    color: var(--danger);
    word-break: break-word;
  }
</style>
