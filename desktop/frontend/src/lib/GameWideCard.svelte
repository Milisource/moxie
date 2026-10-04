<script>
  // Presentational wide card — the F95Zone "Latest Alpha"-style landscape tile
  // (16:9 art with overlaid badges/version, then title + a stats line). Sibling
  // of GameGridCard (portrait cover grid); same prop contract, so GameList can
  // swap renderers per view mode without the parent changing.
  import {engineColor} from './engineColors.js'
  import {library} from './viewState.svelte.js'
  import {statusLabel} from './statuses.js'
  import {isVirtual, hasUpdate, updateUnknown, lastPlayedDate, relativePlayed} from './useLibrarySort.svelte.js'
  import {initials} from './initials.js'
  import {coverRev} from './cover.js'

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
  class="wide-card"
  role="button"
  tabindex={isRoving ? 0 : -1}
  data-game-id={game.id}
  onclick={onOpenDetail}
  onfocus={onFocus}
  onkeydown={(e) => { if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onOpenDetail(); } }}
  oncontextmenu={onContextMenu}
>
  <div class="wide-media" style:--cover-tone={game.coverTone}>
    {#if game.hasCover && coverBase && !library.failedCovers.has(game.id)}
      <img
        class="wide-img"
        src={coverSrc(game.id, 'wide', coverRev(game))}
        alt="{game.title} cover"
        loading="lazy"
        decoding="async"
        onerror={() => markFailed(game.id)}
      />
    {:else}
      <!-- Typographic catalog card: initials on ruled stock. -->
      <span
        class="wide-ph"
        title={game.hasCover && coverBase
          ? 'Cover unavailable — retries after the next sync or cover fetch'
          : 'No cover — use Covers in the sidebar to fetch one'}
      >
        <span class="wide-ph-initials">{initials(game.title)}</span>
        <span class="wide-ph-engine">{game.engine || ''}</span>
      </span>
    {/if}

    <!-- Overlaid classification: engine (left) + status (left), version (right). -->
    <div class="wide-tags">
      {#if game.engine}
        <span class="tag-engine" style="--ec: {engineColor(game.engine)}">{game.engine}</span>
      {/if}
      <span class="tag-status status-{game.status || 'unknown'}">{statusLabel(game.status)}</span>
    </div>

    {#if hasUpdate(game)}
      <span class="wide-version version-update" title="Update available: {game.version} → {game.latestVersion}">
        {game.version} <span class="version-arrow">→</span> {game.latestVersion}
      </span>
    {:else if updateUnknown(game)}
      <span class="wide-version version-unknown" title="Installed version unknown; latest {game.latestVersion}">? → {game.latestVersion}</span>
    {:else if game.version}
      <span class="wide-version">{game.version}</span>
    {/if}

    {#if hasUpdate(game)}
      <span class="update-edge" title="Update available"></span>
    {/if}
    {#if isVirtual(game)}
      <span class="wide-badge badge-virtual">Not installed</span>
    {/if}

    <button
      class="wide-play"
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

  <div class="wide-title" title={game.title}>{game.title}</div>

  <!-- Stats line mirrors F95Zone's time/view/rating row with the signals moxie
       actually has: last played and install size. -->
  <div class="wide-stats">
    <span class="stat-played" title={lastPlayedDate(game) ? `Last played ${lastPlayedDate(game).toLocaleString()}` : 'Never played'}>
      ◷ {lastPlayedDate(game) ? relativePlayed(lastPlayedDate(game)) : 'Never'}
    </span>
    <span class="stat-size" title="Installed size">{game.sizeLabel || '—'}</span>
  </div>
</div>

<style>
  .wide-card {
    cursor: pointer;
    outline: none;
    transition: background 0.1s;
    padding: 6px;
    margin: -6px;
  }
  .wide-card:hover { background: var(--bg-hover); }
  .wide-card:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: 0;
  }

  .wide-media {
    position: relative;
    overflow: hidden;
    /* F95Zone's Latest Alpha tiles are landscape screenshots. Must track
       GameList's updateWideLayout mediaHeight math (also 9/16), which the
       wide virtualizer's row height depends on. */
    aspect-ratio: 16 / 9;
    background: var(--cover-tone, var(--bg-tertiary));
    outline: 1px solid var(--border);
    outline-offset: -1px;
  }
  .wide-card:hover .wide-media { outline-color: var(--rule-strong); }
  .wide-media::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(to top, rgba(10, 8, 5, 0.72), transparent 60%);
    opacity: 0;
    transition: opacity 0.15s;
    pointer-events: none;
  }
  .wide-card:hover .wide-media::after,
  .wide-card:focus-visible .wide-media::after,
  .wide-media:has(.wide-play.state-playing)::after,
  .wide-media:has(.wide-play.state-error)::after {
    opacity: 1;
  }

  .wide-img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .wide-ph {
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    width: 100%;
    height: 100%;
    padding: 8% 6%;
    background:
      repeating-linear-gradient(to bottom, transparent 0 23px, var(--border) 23px 24px),
      var(--bg-tertiary);
    color: var(--text-secondary);
  }
  .wide-ph-initials {
    font-family: var(--font-display);
    font-size: var(--text-4xl);
    font-weight: 600;
    line-height: 1;
    letter-spacing: 0.02em;
    color: var(--text-primary);
    opacity: 0.85;
  }
  .wide-ph-engine {
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
    color: var(--text-muted);
  }

  /* ── Overlaid classification + version ─────────────── */
  .wide-tags {
    position: absolute;
    top: 8px;
    left: 8px;
    right: 8px;
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
    pointer-events: none;
    z-index: 2;
    max-width: calc(100% - 16px);
  }
  .tag-engine,
  .tag-status {
    padding: 1px 6px;
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    line-height: 15px;
    text-transform: uppercase;
    letter-spacing: 0.02em;
    background: rgba(12, 10, 16, 0.72);
    white-space: nowrap;
  }
  .tag-engine {
    color: var(--ec);
    border: 1px solid color-mix(in srgb, var(--ec) 60%, transparent);
  }
  .tag-status {
    color: var(--text-secondary);
    border: 1px solid var(--rule-strong);
  }
  .tag-status.status-active { color: var(--success); border-color: color-mix(in srgb, var(--success) 55%, transparent); }
  .tag-status.status-completed { color: var(--accent); border-color: color-mix(in srgb, var(--accent) 55%, transparent); }
  .tag-status.status-on_hold { color: var(--warning); border-color: color-mix(in srgb, var(--warning) 55%, transparent); }
  .tag-status.status-abandoned { color: var(--text-muted); }
  .tag-status.status-unknown { color: var(--text-muted); }

  .wide-version {
    position: absolute;
    top: 8px;
    right: 8px;
    max-width: calc(100% - 16px);
    padding: 1px 6px;
    background: rgba(12, 10, 16, 0.72);
    border: 1px solid var(--rule-strong);
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    line-height: 15px;
    color: var(--text-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    z-index: 2;
    pointer-events: none;
  }
  .wide-version.version-update { color: var(--accent); border-color: var(--accent-dim); }
  .version-arrow { opacity: 0.7; }
  .wide-version.version-unknown { color: var(--text-muted); }

  /* Update marker: an accent edge down the media's left side. */
  .update-edge {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 4px;
    background: var(--accent);
    z-index: 2;
  }

  .wide-badge {
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
    margin-top: 20px;
    background: rgba(12, 10, 16, 0.85);
    color: var(--text-secondary);
    border: 1px solid var(--rule-strong);
  }

  /* Hover Play (Steam/itch card pattern) */
  .wide-play {
    position: absolute;
    left: 50%;
    top: 50%;
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
  .wide-card:hover .wide-play,
  .wide-card:focus-visible .wide-play,
  .wide-play.state-playing,
  .wide-play.state-error {
    opacity: 1;
    visibility: visible;
  }
  .wide-play:hover { background: var(--accent-hover); }
  .wide-play:disabled { opacity: 0.85; cursor: default; }
  .wide-play.state-launching { background: var(--accent-dim); }
  .wide-play.state-playing {
    background: var(--success);
    color: var(--on-accent);
    animation: pulse-success 1.2s ease-in-out infinite;
  }
  .wide-play.state-error { background: var(--danger); color: var(--on-accent); }

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

  /* Caption: title then a one-line stats row. Heights are fixed and mirrored
     by GameList's wideTextHeight (comfortable 6 + 34 + 4 + 16, compact
     4 + 30 + 3 + 14). */
  .wide-title {
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

  .wide-stats {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 4px;
    height: 16px;
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
    overflow: hidden;
  }
  .stat-played,
  .stat-size {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .stat-size { margin-left: auto; flex-shrink: 0; }

  :global(.density-compact) .wide-title {
    margin-top: 4px;
    height: 30px;
    font-size: var(--text-sm);
    line-height: 15px;
  }
  :global(.density-compact) .wide-stats {
    margin-top: 3px;
    height: 14px;
    gap: 8px;
  }
  :global(.density-compact) .wide-ph-initials { font-size: var(--text-3xl); }
</style>
