// Shared "Edit Game" dialog service. Like confirmDialog.svelte.js /
// promptDialog.svelte.js this is a module-scope $state singleton so both the
// library (context menu) and the detail view can open ONE dialog instance
// mounted at the app root, without prop-drilling an open flag through the
// component tree.
//
// openEditGame(gameId) resolves true when the user saved (callers refresh
// their data) or false when they cancelled/dismissed.

export const editGameState = $state({
  open: false,
  gameId: null,
})

let resolver = null

export function openEditGame(gameId) {
  return new Promise((resolve) => {
    // A dialog already open loses its answer — resolve it false rather than
    // leaving the previous caller's promise dangling.
    if (resolver) resolver(false)
    resolver = resolve
    editGameState.gameId = gameId
    editGameState.open = true
  })
}

export function settleEditGame(saved) {
  editGameState.open = false
  const r = resolver
  resolver = null
  if (r) r(saved)
}
