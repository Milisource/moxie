// Tab-surviving view state.
//
// Views are destroyed on every tab switch (App.svelte renders them inside
// {#if} blocks), so anything interactive that must survive navigation lives
// here as module-scope state: filters, sort order, scroll offsets, in-flight
// results. Long-running operations (sync, scan, covers, updates) already live
// at App level in App.svelte — this module covers the per-view UI state.
//
// Why objects and not individual exports: the Svelte compiler only rewrites
// runes inside one file at a time, so a directly exported `$state` binding
// can't be reassigned from an importing component ("Cannot assign to
// import"). Exporting one stateful object per view keeps property writes
// reactive across modules.

// ── Library list ───────────────────────────────────────────────
export const library = $state({
  search: '',
  engine: 'All',
  status: '',
  sortColumn: 'title',
  sortDesc: false,
  scrollTop: 0,

  // Games whose cover <img> failed to load + a retry epoch: a remounted list
  // re-renders cached 404s from the webview without re-requesting them, so
  // the failure set survives too. A sync/backfill bumps the epoch to force
  // retries.
  failedCovers: new Set(),
  coverEpoch: 0,
})

// ── F95 browser ────────────────────────────────────────────────
export const browser = $state({
  query: '',
  results: [],
  loading: false,
  searched: false,
  error: '',
  selected: null,
  previewing: false,
  preview: null,
  previewError: '',
  expandedOverview: false,
  addResult: null,

  // Shared add-to-library in-flight flag: survives remounts so a mid-add
  // tab switch can't re-enable the button and double-add the same game.
  addingInFlight: false,

  // Monotonic request ids shared across mounts: a stale in-flight response
  // from a destroyed instance must never land in a freshly remounted view.
  // Only a NEWER request (which bumps the shared counter) supersedes an
  // older one — exactly as if the view had never unmounted.
  searchSeq: 0,
  previewSeq: 0,
})

// ── Downloads view ─────────────────────────────────────────────
export const downloads = $state({
  search: '',
  expanded: new Set(),
})

// ── Collections view ───────────────────────────────────────────
export const collectionsView = $state({
  selectedId: null,
})
