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
  // rev (see coverRev) busts the webview's HTTP cache when a cover is
  // replaced — the server sends max-age=3600.
  function coverSrc(id, variant = 'thumb', rev = '') {
    const q = []
    if (library.failedCovers.has(id)) q.push(`r=${library.coverEpoch}`)
    if (rev) q.push(`v=${rev}`)
    return `${getCoverBase()}/cover/${id}/${variant}${q.length ? '?' + q.join('&') : ''}`
  }

  function markFailed(id) {
    library.failedCovers = new Set([...library.failedCovers, id])
  }

  return {coverSrc, markFailed}
}

/** Cache-busting token for a game's current cover (size + provenance). */
export function coverRev(game) {
  return game?.coverW ? `${game.coverW}x${game.coverH}${game.coverSource ? '-' + game.coverSource : ''}` : ''
}

/**
 * How the 3:4 grid card should fit a cover: wide banners (thumbs are
 * uncropped above aspect 1.6, see desktop/covermeta.go) and tiny images are
 * letterboxed over their tone instead of blown up into a crop.
 */
export function coverFit(game) {
  const w = game?.coverW, h = game?.coverH
  if (!w || !h) return 'cover'
  if (w / h > 1.6) return 'contain'
  if (Math.min(w, h) < 240) return 'contain'
  return 'cover'
}
