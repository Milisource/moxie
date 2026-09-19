<script>
  // Presentational grid card — extracted out of GameList.svelte's cover-grid
  // branch. Owns only per-card markup/CSS; sort/filter/nav state stays in
  // the parent, which supplies playControl/coverSrc/callbacks as props.
  import {engineColor} from './engineColors.js'
  import {library} from './viewState.svelte.js'
  import {isVirtual, hasUpdate, lastPlayedDate, relativePlayed} from './useLibrarySort.svelte.js'

  let {
    game,
    playControl,
    isRoving = false,
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

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions a11y_no_noninteractive_tabindex -->
<div
  class="card"
  role="button"
  tabindex={isRoving ? 0 : -1}
  data-game-id={game.id}
  onclick={onOpenDetail}
  onfocus={onFocus}
  onkeydown={(e) => { if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onOpenDetail(); } }}
  oncontextmenu={onContextMenu}
>
  <div class="cover-frame">
    {#if game.hasCover && coverBase && !library.failedCovers.has(game.id)}
      <img
        class="cover-img"
        src={coverSrc(game.id)}
        alt="{game.title} cover"
        loading="lazy"
        onerror={() => markFailed(game.id)}
      />
    {:else if game.hasCover && coverBase}
      <span class="cover-ph" title="Cover unavailable — retries after the next sync or cover fetch">
        <span class="cover-ph-icon">⊠</span>
      </span>
    {:else}
      <span class="cover-ph" title="No cover — use Covers in the sidebar to fetch one">
        <span class="cover-ph-icon">▭</span>
      </span>
    {/if}

    {#if hasUpdate(game)}
      <span class="card-badge badge-update" title="Update available: {game.latestVersion}">
        ⇪ {game.version} → {game.latestVersion}
      </span>
    {/if}
    {#if isVirtual(game)}
      <span class="card-badge badge-virtual">Not installed</span>
    {/if}

    <button
      class="card-play"
      class:state-launching={ctl.state === 'launching'}
      class:state-playing={ctl.state === 'playing'}
      class:state-error={ctl.state === 'error'}
      disabled={ctl.state === 'launching'}
      title={ctl.msg || 'Play'}
      onclick={onPlay}
    >
      {#if ctl.state === 'launching'}
        <span class="play-spinner"></span><span>Launching…</span>
      {:else if ctl.state === 'playing'}
        <span>⏸ Playing</span>
      {:else if ctl.state === 'error'}
        <span>↻ Retry</span>
      {:else}
        <span>▶ Play</span>
      {/if}
    </button>
  </div>

  <div class="card-title" title={game.title}>{game.title}</div>
  <div class="card-meta">
    {#if game.engine}
      <span class="card-engine" style="--ec: {engineColor(game.engine)}">{game.engine}</span>
    {/if}
    {#if lastPlayedDate(game)}
      <span class="card-last-played" title="Last played {lastPlayedDate(game).toLocaleString()}">
        ▶ {relativePlayed(lastPlayedDate(game))}
      </span>
    {/if}
  </div>
</div>

<style>
  .card {
    cursor: pointer;
    border-radius: 12px;
    outline: none;
    transition: background 0.1s;
    padding: 6px;
    margin: -6px;
  }
  .card:hover { background: var(--bg-hover); }
  .card:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .cover-frame {
    position: relative;
    border-radius: 10px;
    overflow: hidden;
    aspect-ratio: 16 / 9;
    background: var(--bg-tertiary);
    box-shadow: 0 2px 10px rgba(0, 0, 0, 0.35);
  }
  .cover-frame::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(to top, rgba(0, 0, 0, 0.65), transparent 55%);
    opacity: 0;
    transition: opacity 0.15s;
    pointer-events: none;
  }
  .card:hover .cover-frame::after,
  .card:focus-visible .cover-frame::after,
  .cover-frame:has(.card-play.state-playing)::after,
  .cover-frame:has(.card-play.state-error)::after {
    opacity: 1;
  }

  .cover-img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .cover-ph {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    background: var(--bg-tertiary);
  }
  .cover-ph-icon { font-size: 28px; opacity: 0.3; }

  /* Play-state badges on the cover (update / not installed) */
  .card-badge {
    position: absolute;
    top: 8px;
    padding: 2px 8px;
    border-radius: 10px;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.02em;
    pointer-events: none;
    z-index: 2;
  }
  .badge-update {
    left: 8px;
    background: var(--warning);
    color: #1a1a10;
  }
  .badge-virtual {
    right: 8px;
    background: color-mix(in srgb, var(--text-muted) 85%, transparent);
    color: #fff;
  }

  /* Hover Play (Steam/itch card pattern) */
  .card-play {
    position: absolute;
    left: 50%;
    top: 56%;
    transform: translate(-50%, -50%);
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 18px;
    border: none;
    border-radius: 8px;
    background: var(--accent);
    color: #fff;
    font-size: 13px;
    font-weight: 700;
    cursor: pointer;
    z-index: 3;
    box-shadow: 0 4px 18px rgba(0, 0, 0, 0.5);
    opacity: 0;
    visibility: hidden;
    transition: opacity 0.15s, transform 0.15s, background 0.12s;
  }
  .card:hover .card-play,
  .card:focus-visible .card-play,
  .card-play.state-playing,
  .card-play.state-error {
    opacity: 1;
    visibility: visible;
  }
  .card:hover .card-play { transform: translate(-50%, -50%) scale(1.04); }
  .card-play:hover { background: var(--accent-hover); }
  .card-play:disabled { opacity: 0.85; cursor: default; }
  .card-play.state-launching { background: var(--accent-dim); }
  .card-play.state-playing {
    background: var(--success);
    color: #07140b;
    opacity: 1;
    visibility: visible;
    animation: pulse-success 1.2s ease-in-out infinite;
  }
  .card-play.state-error { background: var(--danger); opacity: 1; visibility: visible; }

  @keyframes pulse-success {
    0%, 100% { box-shadow: 0 4px 18px rgba(74, 222, 128, 0.35); }
    50%      { box-shadow: 0 4px 26px rgba(74, 222, 128, 0.7); }
  }

  .play-spinner {
    width: 12px;
    height: 12px;
    border: 2px solid rgba(255, 255, 255, 0.5);
    border-top-color: #fff;
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .card-title {
    margin-top: 8px;
    font-size: 13px;
    font-weight: 500;
    color: var(--text-primary);
    line-height: 1.25;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    min-height: 2.5em;
  }

  .card-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 4px;
    min-height: 18px;
  }
  .card-engine {
    display: inline-block;
    padding: 1px 7px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 600;
    background: color-mix(in srgb, var(--ec) 15%, transparent);
    color: var(--ec);
  }
  .card-last-played {
    font-size: 10px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  /* Density: compact (P1 item 7) — mirrors GameList's gridTextHeight, which
     the virtualizer's row-height math depends on; change one, change both. */
  :global(.density-compact) .card-title {
    margin-top: 6px;
    font-size: 11px;
    min-height: 32px;
  }
  :global(.density-compact) .card-meta {
    margin-top: 3px;
    min-height: 16px;
    gap: 6px;
  }
</style>
