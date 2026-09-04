<script>
  import {GetUpdatableCount, GetCollections} from '../../wailsjs/go/main/App'

  let {version = '', activeView = $bindable('library'), onNavigate, lastUpdate = 0} = $props()

  let updateCount = $state(null)      // null = not loaded, 0+ = loaded
  let collectionCount = $state(null)

  async function loadCount() {
    try {
      updateCount = await GetUpdatableCount()
    } catch (e) {
      updateCount = 0
    }
    try {
      collectionCount = ((await GetCollections()) || []).length
    } catch (e) {
      collectionCount = 0
    }
  }

  // Load on mount and re-fetch every time the user navigates
  // (in case updates were applied elsewhere).
  $effect(() => {
    if (activeView || lastUpdate) loadCount()
  })

  // Primary items get the most visual weight — these are the two views a
  // user lives in day-to-day. Everything else is either a regular daily
  // action (still always visible) or an occasional management action
  // (demoted, collapsed behind a disclosure toggle by default).
  // Icons used to live here (a mix of emoji and Unicode symbols that never
  // rendered consistently across platforms/fonts). Nav rows are text-only —
  // hover/active background is the affordance instead (docs/desktop-ui-
  // research.md §7, icon-consistency cleanup).
  const primaryItems = [
    {id: 'library', label: 'Library'},
    {id: 'browser', label: 'Browse'},
  ]

  let regularItems = $derived.by(() => [
    {id: 'add', label: 'Add Game'},
    {id: 'updates', label: 'Updates', badge: updateCount},
    {id: 'downloads', label: 'Downloads'},
    {id: 'collections', label: 'Collections', badge: collectionCount},
  ])

  const manageItems = [
    {id: 'scan', label: 'Scan'},
    {id: 'sync', label: 'Sync'},
    {id: 'covers', label: 'Covers'},
    {id: 'duplicates', label: 'Duplicates'},
    {id: 'trash', label: 'Trash'},
    {id: 'settings', label: 'Settings'},
  ]

  const MANAGE_STORAGE_KEY = 'sidebar-manage-expanded'
  let manageExpanded = $state(localStorage.getItem(MANAGE_STORAGE_KEY) === 'true')

  function toggleManage() {
    manageExpanded = !manageExpanded
    localStorage.setItem(MANAGE_STORAGE_KEY, String(manageExpanded))
  }

  // If the user lands directly on a management view (e.g. deep link, or
  // returning to a session), auto-expand so the active item stays visible.
  $effect(() => {
    if (manageItems.some((item) => item.id === activeView)) manageExpanded = true
  })
</script>

<aside class="sidebar">
  <div class="brand">
    <span class="brand-icon">◆</span>
    <span class="brand-text">Moxie</span>
  </div>

  <nav class="nav">
    {#each primaryItems as item}
      <button
        class="nav-item nav-item-primary"
        class:active={activeView === item.id}
        onclick={() => onNavigate?.(item.id)}
      >
        <span class="nav-label">{item.label}</span>
      </button>
    {/each}

    <div class="nav-divider"></div>

    {#each regularItems as item}
      <button
        class="nav-item"
        class:active={activeView === item.id}
        onclick={() => onNavigate?.(item.id)}
      >
        <span class="nav-label">{item.label}</span>
        {#if item.badge !== null && item.badge !== undefined && item.badge > 0}
          <span class="nav-badge">{item.badge}</span>
        {/if}
      </button>
    {/each}

    <button class="nav-manage-toggle" onclick={toggleManage} aria-expanded={manageExpanded}>
      <span class="nav-manage-chevron" class:expanded={manageExpanded}>›</span>
      <span>Manage</span>
    </button>

    {#if manageExpanded}
      {#each manageItems as item}
        <button
          class="nav-item nav-item-demoted"
          class:active={activeView === item.id}
          onclick={() => onNavigate?.(item.id)}
        >
          <span class="nav-label">{item.label}</span>
        </button>
      {/each}
    {/if}
  </nav>

  <div class="sidebar-footer">
    {#if version}
      <span class="version">v{version}</span>
    {/if}
  </div>
</aside>

<style>
  .sidebar {
    width: var(--sidebar-width);
    height: 100vh;
    display: flex;
    flex-direction: column;
    background: var(--bg-secondary);
    border-right: 1px solid var(--border);
    flex-shrink: 0;
    user-select: none;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    height: var(--header-height);
    padding: 0 20px;
    border-bottom: 1px solid var(--border);
  }

  .brand-icon {
    font-size: 18px;
    color: var(--accent);
  }

  .brand-text {
    font-size: 16px;
    font-weight: 700;
    color: var(--text-primary);
    letter-spacing: 0.02em;
  }

  .nav {
    flex: 1;
    padding: var(--space-3) var(--space-3);
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .nav-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: var(--space-3) var(--space-4);
    border: none;
    border-left: 2px solid transparent;
    border-radius: 6px;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--text-base);
    cursor: pointer;
    text-align: left;
    transition: all 0.12s;
  }

  /* Text-only rows (no per-item icon, docs/desktop-ui-research.md §7
     icon-consistency cleanup) — hover/active background + accent edge are
     the whole affordance, so both need to read clearly at a glance. */
  .nav-item:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .nav-item.active {
    background: var(--accent-dim);
    border-left-color: var(--accent);
    color: #fff;
  }

  /* Primary items (Library, Browse) carry the most visual weight — bigger,
     bolder text, a touch more breathing room — since the sidebar frames
     every screen and these are the two views used constantly. */
  .nav-item-primary {
    padding: var(--space-4);
    font-size: var(--text-md);
    font-weight: 600;
  }

  .nav-item-primary.active {
    box-shadow: var(--shadow-sm);
  }

  .nav-divider {
    height: 1px;
    margin: var(--space-3) var(--space-2);
    background: var(--border);
  }

  /* Demoted management items (Scan/Sync/Covers/Duplicates/Trash/Settings) —
     lighter text, smaller icon, tighter padding — sit behind the "Manage"
     disclosure toggle below so they don't compete with daily-use items. */
  .nav-item-demoted {
    padding: var(--space-2) var(--space-4);
    font-size: var(--text-sm);
    color: var(--text-muted);
  }

  .nav-item-demoted:hover {
    color: var(--text-secondary);
  }

  .nav-manage-toggle {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-top: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border: none;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--text-xs);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    cursor: pointer;
    text-align: left;
  }

  .nav-manage-toggle:hover {
    color: var(--text-secondary);
  }

  .nav-manage-chevron {
    display: inline-block;
    font-size: var(--text-base);
    transition: transform 0.12s;
  }

  .nav-manage-chevron.expanded {
    transform: rotate(90deg);
  }

  .nav-label {
    font-weight: 500;
  }

  .nav-badge {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    border-radius: 9px;
    background: var(--warning);
    color: #000;
    font-size: 10px;
    font-weight: 700;
    line-height: 1;
  }

  .sidebar-footer {
    padding: 12px 20px;
    border-top: 1px solid var(--border);
  }

  .version {
    font-size: 11px;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
</style>
