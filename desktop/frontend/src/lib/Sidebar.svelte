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
    <span class="brand-text">Moxie</span>
    <span class="brand-sub">Library index</span>
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
  /* Index-style navigation: hairline-separated rows, an amber ▌ marker on
     the active row, condensed uppercase labels (docs/desktop-ui-research.md §8). */
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
    align-items: baseline;
    gap: 10px;
    height: var(--header-height);
    padding: 0 20px;
    border-bottom: 1px solid var(--border);
    line-height: var(--header-height);
  }
  .brand-text {
    font-family: var(--font-display);
    font-size: var(--text-xl);
    font-weight: 700;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--accent);
  }
  .brand-sub {
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .nav {
    flex: 1;
    padding: var(--space-4) 0;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
  }

  .nav-item {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 20px;
    border: none;
    border-bottom: 1px solid var(--border);
    background: transparent;
    color: var(--text-secondary);
    font-family: var(--font-display);
    font-size: var(--text-md);
    font-weight: 500;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    cursor: pointer;
    text-align: left;
    transition: background 0.1s, color 0.1s;
  }
  .nav-item:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
  .nav-item.active {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }
  .nav-item.active::before {
    content: '▌';
    position: absolute;
    left: 4px;
    color: var(--accent);
  }

  .nav-item-primary {
    padding: 12px 20px;
    font-size: var(--text-lg);
    font-weight: 600;
  }
  .nav-item-primary:first-child { border-top: 1px solid var(--border); }

  .nav-divider {
    height: var(--space-5);
  }

  .nav-item-demoted {
    padding: 6px 20px;
    font-family: var(--font-sans);
    font-size: var(--text-sm);
    text-transform: none;
    letter-spacing: 0;
    color: var(--text-muted);
  }
  .nav-item-demoted:hover { color: var(--text-secondary); }

  .nav-manage-toggle {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-top: var(--space-5);
    padding: var(--space-2) 20px;
    border: none;
    border-bottom: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    font-family: var(--font-display);
    font-size: var(--text-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
    cursor: pointer;
    text-align: left;
  }
  .nav-manage-toggle:hover { color: var(--text-secondary); }

  .nav-manage-chevron {
    display: inline-block;
    font-size: var(--text-base);
    transition: transform 0.12s;
  }
  .nav-manage-chevron.expanded { transform: rotate(90deg); }

  .nav-label { font-weight: inherit; }

  .nav-badge {
    margin-left: auto;
    min-width: 22px;
    padding: 0 4px;
    border: 1px solid var(--accent-dim);
    color: var(--accent);
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    line-height: 16px;
    text-align: center;
    letter-spacing: 0;
    font-variant-numeric: tabular-nums;
  }

  .sidebar-footer {
    padding: 12px 20px;
    border-top: 1px solid var(--border);
  }
  .version {
    font-size: var(--text-xs);
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
</style>
