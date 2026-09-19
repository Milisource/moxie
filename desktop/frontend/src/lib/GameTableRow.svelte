<script>
  // Presentational table row — extracted out of GameList.svelte's list-view
  // branch. Owns only per-row markup/CSS; sort/filter/nav state stays in
  // the parent, which supplies playControl/coverSrc/callbacks as props.
  import {engineColor} from './engineColors.js'
  import {library} from './viewState.svelte.js'
  import {statusLabel} from './statuses.js'
  import {hasUpdate} from './useLibrarySort.svelte.js'

  let {
    game,
    playControl,
    isRoving = false,
    top = 0,
    height = 49,
    coverBase,
    coverSrc,
    markFailed,
    onOpenDetail = () => {},
    onFocus = () => {},
    onContextMenu = () => {},
    onPlay = () => {},
  } = $props()

  let ctl = $derived(playControl(game))
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div
  class="table-row"
  style="height: {height}px; transform: translateY({top}px)"
  data-game-id={game.id}
  onclick={onOpenDetail}
  onfocus={onFocus}
  oncontextmenu={onContextMenu}
  onkeydown={(e) => { if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onOpenDetail(); } }}
  role="button"
  tabindex={isRoving ? 0 : -1}
>
  <span class="col-cover">
    {#if game.hasCover && coverBase && !library.failedCovers.has(game.id)}
      <img
        class="cover-thumb"
        src={coverSrc(game.id)}
        alt="{game.title} cover"
        loading="lazy"
        onerror={() => markFailed(game.id)}
      />
    {:else if game.hasCover && coverBase}
      <span class="cover-placeholder" title="Cover unavailable — retries after the next sync or cover fetch">
        <span class="cover-icon">⊠</span>
      </span>
    {:else}
      <span class="cover-placeholder" title="No cover — use Covers in the sidebar to fetch one">
        <span class="cover-icon">▭</span>
      </span>
    {/if}
  </span>
  <span class="col-title game-title">
    {game.title}
    {#if hasUpdate(game)}
      <span class="update-dot" title="Update available: {game.latestVersion}">●</span>
    {/if}
  </span>
  <span class="col-engine">
    {#if game.engine}
      <span class="engine-badge" style="--ec: {engineColor(game.engine)}">
        {game.engine}
      </span>
    {:else}
      <span class="text-muted">—</span>
    {/if}
  </span>
  <span class="col-version">
    {#if hasUpdate(game)}
      <span class="version-old">{game.version}</span>
      <span class="version-new" title="Latest: {game.latestVersion}">→ {game.latestVersion}</span>
    {:else}
      {game.version || '—'}
    {/if}
  </span>
  <span class="col-size">{game.sizeLabel || '—'}</span>
  <span class="col-status">
    <span class="status-badge status-{game.status || 'unknown'}">
      {statusLabel(game.status)}
    </span>
  </span>
  <span class="col-play">
    <button
      class="row-play"
      class:state-launching={ctl.state === 'launching'}
      class:state-playing={ctl.state === 'playing'}
      class:state-error={ctl.state === 'error'}
      disabled={ctl.state === 'launching'}
      title={ctl.msg || 'Play'}
      onclick={onPlay}
    >
      {#if ctl.state === 'launching'}
        <span class="play-spinner small"></span>
      {:else if ctl.state === 'playing'}
        ⏸
      {:else if ctl.state === 'error'}
        ↻
      {:else}
        ▶ Play
      {/if}
    </button>
  </span>
</div>

<style>
  .table-row {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    display: grid;
    grid-template-columns: 80px 1fr 110px 130px 80px 100px 64px;
    gap: 8px;
    padding: 4px 12px;
    font-size: 13px;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    transition: background 0.08s;
    align-items: center;
    box-sizing: border-box;
  }
  .table-row:hover { background: var(--bg-hover); }
  .table-row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  :global(.density-compact) .table-row { padding: 2px 12px; font-size: 11px; }
  :global(.density-compact) .cover-thumb,
  :global(.density-compact) .cover-placeholder { width: 44px; height: 25px; }
  :global(.density-compact) .cover-icon { font-size: 12px; }

  .game-title {
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .update-dot {
    display: inline-block;
    margin-left: 4px;
    color: var(--warning);
    font-size: 10px;
    vertical-align: super;
  }

  .engine-badge {
    display: inline-block;
    padding: 1px 7px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 600;
    background: color-mix(in srgb, var(--ec) 15%, transparent);
    color: var(--ec);
  }

  .version-old {
    text-decoration: line-through;
    opacity: 0.7;
    margin-right: 4px;
  }
  .version-new {
    color: var(--warning);
    font-weight: 600;
    font-size: 12px;
  }

  .col-size { font-size: 12px; color: var(--text-secondary); }

  .col-play {
    display: flex;
    align-items: center;
    justify-content: flex-start;
  }
  .row-play {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    border: 1px solid var(--accent-dim);
    border-radius: 6px;
    background: color-mix(in srgb, var(--accent) 10%, transparent);
    color: var(--accent);
    font-size: 11px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.12s;
    white-space: nowrap;
  }
  .row-play:hover { background: var(--accent); color: #fff; border-color: var(--accent); }
  .row-play:disabled { opacity: 0.7; cursor: default; }
  .row-play.state-playing { background: var(--success); color: #07140b; border-color: var(--success); }
  .row-play.state-error { background: var(--danger); color: #fff; border-color: var(--danger); }

  .play-spinner {
    width: 12px;
    height: 12px;
    border: 2px solid rgba(255, 255, 255, 0.5);
    border-top-color: #fff;
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  .play-spinner.small { width: 10px; height: 10px; }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* ── Cover Column ───────────────────────────────────── */
  .col-cover {
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 10px;
    color: var(--text-muted);
  }

  .cover-thumb {
    display: block;
    width: 72px;
    height: 40px;
    object-fit: cover;
    border-radius: 3px;
    background: var(--bg-secondary);
    flex-shrink: 0;
  }

  .cover-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 72px;
    height: 40px;
    border-radius: 3px;
    background: var(--bg-tertiary);
    flex-shrink: 0;
  }

  .cover-icon {
    font-size: 16px;
    opacity: 0.3;
  }

  .status-badge {
    display: inline-block;
    padding: 1px 7px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 500;
  }
  .status-active { background: color-mix(in srgb, var(--success) 15%, transparent); color: var(--success); }
  .status-completed { background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); }
  .status-abandoned { background: color-mix(in srgb, var(--text-muted) 15%, transparent); color: var(--text-muted); }
  .status-on_hold { background: color-mix(in srgb, var(--warning) 15%, transparent); color: var(--warning); }
  .status-unknown { background: color-mix(in srgb, var(--text-muted) 10%, transparent); color: var(--text-muted); }

  .text-muted { color: var(--text-muted); }
</style>
