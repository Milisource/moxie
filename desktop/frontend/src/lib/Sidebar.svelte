<script>
  import {GetUpdatableCount} from '../../wailsjs/go/main/App'

  let {version = '', activeView = $bindable('library'), onNavigate, lastUpdate = 0} = $props()

  let updateCount = $state(null)   // null = not loaded, 0+ = loaded

  async function loadCount() {
    try {
      updateCount = await GetUpdatableCount()
    } catch (e) {
      updateCount = 0
    }
  }

  // Load on mount and re-fetch every time the user navigates
  // (in case updates were applied elsewhere).
  $effect(() => {
    if (activeView || lastUpdate) loadCount()
  })

  let sections = $derived.by(() => [
    {
      label: 'Library',
      items: [
        {id: 'library', label: 'Library', icon: '▦'},
        {id: 'browser', label: 'Browse', icon: '🌐'},
        {id: 'scan', label: 'Scan', icon: '⊕'},
        {id: 'add', label: 'Add Game', icon: '+'},
        {id: 'updates', label: 'Updates', icon: '↻', badge: updateCount},
        {id: 'sync', label: 'Sync', icon: '⟳'},
      ],
    },
    {
      label: 'Media',
      items: [
        {id: 'downloads', label: 'Downloads', icon: '↓'},
        {id: 'steam', label: 'Steam', icon: '◈'},
      ],
    },
    {
      label: 'Management',
      items: [
        {id: 'duplicates', label: 'Duplicates', icon: '◎'},
        {id: 'trash', label: 'Trash', icon: '🗑'},
        {id: 'settings', label: 'Settings', icon: '⚙'},
      ],
    },
  ])
</script>

<aside class="sidebar">
  <div class="brand">
    <span class="brand-icon">◆</span>
    <span class="brand-text">Moxie</span>
  </div>

  <nav class="nav">
    {#each sections as section}
      <span class="nav-section-label">{section.label}</span>
      {#each section.items as item}
        <button
          class="nav-item"
          class:active={activeView === item.id}
          onclick={() => onNavigate?.(item.id)}
        >
          <span class="nav-icon">{item.icon}</span>
          <span class="nav-label">{item.label}</span>
          {#if item.badge !== null && item.badge !== undefined && item.badge > 0}
            <span class="nav-badge">{item.badge}</span>
          {/if}
        </button>
      {/each}
    {/each}
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
    padding: 6px 8px;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .nav-section-label {
    padding: 10px 12px 4px;
    font-size: 10px;
    font-weight: 700;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .nav-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--text-secondary);
    font-size: 13px;
    cursor: pointer;
    text-align: left;
    transition: all 0.12s;
  }

  .nav-item:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .nav-item.active {
    background: var(--accent-dim);
    color: #fff;
  }

  .nav-icon {
    width: 20px;
    text-align: center;
    font-size: 14px;
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
