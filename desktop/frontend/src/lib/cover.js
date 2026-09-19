/**
 * Shared cover-URL helpers — was previously copy-pasted into GameList.svelte
 * and CollectionsView.svelte (both talk to the same loopback-HTTP cover
 * server; see GetCoverBaseURL). Single source of truth now.
 *
 * failedCovers/coverEpoch live in the shared `library` viewState object (not
 * here) so a retry triggered from one tab clears stale 404s cached by
 * another — this module just reads/writes that shared state.
 */
import {library} from './viewState.svelte.js'

/**
 * Binds cover-URL helpers to a component's own `coverBase` (each view fetches
 * its own via GetCoverBaseURL onMount). `getCoverBase` is a closure so the
 * helpers keep reading the live value after coverBase resolves, not an
 * empty-string snapshot taken before the async fetch completes.
 */
export function makeCoverHelpers(getCoverBase) {
  function coverSrc(id, variant = 'thumb') {
    const epoch = library.failedCovers.has(id) ? `?r=${library.coverEpoch}` : ''
    return `${getCoverBase()}/cover/${id}/${variant}${epoch}`
  }

  function markFailed(id) {
    library.failedCovers = new Set([...library.failedCovers, id])
  }

  return {coverSrc, markFailed}
}
