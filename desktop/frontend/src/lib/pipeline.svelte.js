/**
 * Shared "backend pipeline" state factory — was previously hand-rolled per
 * pipeline (sync, cover backfill, app self-update, game install) as its own
 * $state object plus its own block of individually-declared `let unsubX`
 * variables, EventsOn subscriptions, and teardown calls in App.svelte's
 * onMount. All four wanted the same thing: a live $state object that a
 * handful of backend events mutate, plus one cleanup function.
 *
 * Each pipeline's own start/cancel functions (which differ in arguments and
 * backend call — SyncAllGames(force) vs FetchCovers() vs
 * InstallGame(gameId, dest)) stay next to their state in App.svelte, since
 * forcing a one-size start(...args)/cancel() signature onto them would hide
 * real differences behind a shape that doesn't actually fit.
 *
 * Pipelines that don't fit even the state+events part — the per-game update
 * map (gameStates) and its batch/retry-queue orchestration, and the
 * directory-watcher scan state — are keyed or sequenced differently than a
 * single flat state object, so they keep their own hand-rolled wiring.
 */
import {EventsOn} from '../../wailsjs/runtime/runtime'

/**
 * @param {object} initialState - initial pipeline $state shape
 * @param {Record<string, (state: object, data: any) => void>} handlers -
 *   event name -> mutator, called with the live state object and payload
 * @returns {{state: object, unsubscribe: () => void}}
 */
export function createPipeline(initialState, handlers) {
  const state = $state({...initialState})
  const subs = Object.entries(handlers).map(([event, handler]) =>
    EventsOn(event, (data) => handler(state, data))
  )
  return {
    state,
    unsubscribe: () => subs.forEach(unsub => unsub()),
  }
}
