// Shared confirm-dialog service — a Promise-returning drop-in replacement
// for window.confirm(), backed by a single AlertDialog instance mounted
// once at the app root (see ConfirmDialog.svelte). Module-scope $state
// singleton for the same reason viewState.svelte.js uses one: only one
// confirmation can be open at a time, and every call site just awaits
// confirmAction(...) exactly like it used to await window.confirm(...).

export const confirmState = $state({
  open: false,
  title: '',
  description: '',
  confirmLabel: 'Confirm',
  danger: false,
})

let resolver = null

// confirmAction resolves true/false exactly like window.confirm did — the
// caller still performs the guarded action (and its own error handling)
// after the promise resolves, unchanged from the window.confirm call sites
// it replaces.
export function confirmAction({title, description = '', confirmLabel = 'Confirm', danger = false}) {
  return new Promise((resolve) => {
    // A confirmation already open loses its answer — resolve it false
    // rather than leaving the promise dangling.
    if (resolver) resolver(false)
    resolver = resolve
    confirmState.title = title
    confirmState.description = description
    confirmState.confirmLabel = confirmLabel
    confirmState.danger = danger
    confirmState.open = true
  })
}

export function settleConfirm(value) {
  confirmState.open = false
  const r = resolver
  resolver = null
  if (r) r(value)
}
