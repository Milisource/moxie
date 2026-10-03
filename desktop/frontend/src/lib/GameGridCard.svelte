<script>
  // Presentational grid card — extracted out of GameList.svelte's cover-grid
  // branch. Owns only per-card markup/CSS; sort/filter/nav state stays in
  // the parent, which supplies playControl/coverSrc/callbacks as props.
  import {engineColor} from './engineColors.js'
  import {library} from './viewState.svelte.js'
  import {isVirtual, hasUpdate, updateUnknown, lastPlayedDate, relativePlayed} from './useLibrarySort.svelte.js'
  import {initials} from './initials.js'

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
    {:else}
      <!-- Typographic catalog card: initials on ruled stock. -->
      <span
        class="cover-ph"
        title={game.hasCover && coverBase
          ? 'Cover unavailable — retries after the next sync or cover fetch'
          : 'No cover — use Covers in the sidebar to fetch one'}
      >
        <span class="cover-ph-initials">{initials(game.title)}</span>
        <span class="cover-ph-engine">{game.engine || ''}</span>
      </span>
    {/if}

    {#if hasUpdate(game)}
      <span class="update-edge" title="Update available: {game.version} → {game.latestVersion}"></span>
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
        <span>‖ Playing</span>
      {:else if ctl.state === 'error'}
        <span>↻ Retry</span>
      {:else}
        <span>▶︎ Play</span>
      {/if}
    </button>
  </div>

  <div class="card-meta">
    {#if game.engine}
      <span class="card-engine" style="--ec: {engineColor(game.engine)}">{game.engine}</span>
    {/if}
    {#if hasUpdate(game)}
      <span class="card-version card-version-update" title="Update available">
        {game.version} → {game.latestVersion}
      </span>
    {:else if updateUnknown(game)}
      <span class="card-version" title="Installed version unknown; latest {game.latestVersion}">? → {game.latestVersion}</span>
    {:else if lastPlayedDate(game)}
      <span class="card-version" title="Last played {lastPlayedDate(game).toLocaleString()}">
        {relativePlayed(lastPlayedDate(game))}
      </span>
    {:else if game.version}
      <span class="card-version">{game.version}</span>
    {/if}
  </div>
  <div class="card-title" title={game.title}>{game.title}</div>
</div>

<style>
  .card {
    cursor: pointer;
    outline: none;
    transition: background 0.1s;
    padding: 6px;
    margin: -6px;
  }
  .card:hover { background: var(--bg-hover); }
  .card:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: 0;
  }

  .cover-frame {
    position: relative;
    overflow: hidden;
    /* F95Zone covers are native 3:4 portrait art. Must track GameList's
       updateGridLayout coverHeight math (also 4/3), which the grid
       virtualizer's row height depends on. */
    aspect-ratio: 3 / 4;
    background: var(--bg-tertiary);
    outline: 1px solid var(--border);
    outline-offset: -1px;
  }
  .card:hover .cover-frame { outline-color: var(--rule-strong); }
  .cover-frame::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(to top, rgba(10, 8, 5, 0.7), transparent 55%);
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
    flex-direction: column;
    justify-content: space-between;
    width: 100%;
    height: 100%;
    padding: 12% 10%;
    background:
      repeating-linear-gradient(to bottom, transparent 0 23px, var(--border) 23px 24px),
      var(--bg-tertiary);
    color: var(--text-secondary);
  }
  .cover-ph-initials {
    font-family: var(--font-display);
    font-size: var(--text-4xl);
    font-weight: 600;
    line-height: 1;
    letter-spacing: 0.02em;
    color: var(--text-primary);
    opacity: 0.85;
  }
  .cover-ph-engine {
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
    color: var(--text-muted);
  }

  /* Update marker: an accent edge down the cover's left side. */
  .update-edge {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 4px;
    background: var(--accent);
    z-index: 2;
  }

  .card-badge {
    position: absolute;
    top: 8px;
    padding: 2px 6px;
    font-family: var(--font-display);
    font-size: var(--text-2xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
    pointer-events: none;
    z-index: 2;
  }
  .badge-virtual {
    right: 8px;
    background: var(--bg-primary);
    color: var(--text-secondary);
    border: 1px solid var(--rule-strong);
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
    border-radius: var(--radius-1);
    background: var(--accent);
    color: var(--on-accent);
    font-family: var(--font-display);
    font-size: var(--text-sm);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
    cursor: pointer;
    z-index: 3;
    opacity: 0;
    visibility: hidden;
    transition: opacity 0.15s, background 0.12s;
  }
  .card:hover .card-play,
  .card:focus-visible .card-play,
  .card-play.state-playing,
  .card-play.state-error {
    opacity: 1;
    visibility: visible;
  }
  .card-play:hover { background: var(--accent-hover); }
  .card-play:disabled { opacity: 0.85; cursor: default; }
  .card-play.state-launching { background: var(--accent-dim); }
  .card-play.state-playing {
    background: var(--success);
    color: var(--on-accent);
    animation: pulse-success 1.2s ease-in-out infinite;
  }
  .card-play.state-error { background: var(--danger); color: var(--on-accent); }

  @keyframes pulse-success {
    0%, 100% { opacity: 1; }
    50%      { opacity: 0.8; }
  }

  .play-spinner {
    width: 12px;
    height: 12px;
    border: 2px solid color-mix(in srgb, var(--on-accent) 40%, transparent);
    border-top-color: var(--on-accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* Caption: index line (engine, version) then title. Heights are fixed
     and mirrored by GameList's gridTextHeight (comfortable 8 + 18 + 6 + 34,
     compact 6 + 16 + 4 + 30). */
  .card-title {
    margin-top: 6px;
    height: 34px;
    font-family: var(--font-display);
    font-size: var(--text-md);
    font-weight: 500;
    line-height: 17px;
    color: var(--text-primary);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .card-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
    height: 18px;
    overflow: hidden;
  }
  .card-engine {
    flex-shrink: 0;
    padding: 0 5px;
    border: 1px solid color-mix(in srgb, var(--ec) 55%, transparent);
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    line-height: 16px;
    text-transform: uppercase;
    color: var(--ec);
  }
  .card-version {
    min-width: 0;
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-variant-numeric: tabular-nums;
  }
  .card-version-update { color: var(--accent); }

  :global(.density-compact) .card-title {
    margin-top: 4px;
    height: 30px;
    font-size: var(--text-sm);
    line-height: 15px;
  }
  :global(.density-compact) .card-meta {
    margin-top: 6px;
    height: 16px;
    gap: 6px;
  }
  :global(.density-compact) .cover-ph-initials { font-size: var(--text-3xl); }
</style>
