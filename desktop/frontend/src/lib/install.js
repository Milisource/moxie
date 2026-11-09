// Human-readable label for a game-install pipeline phase. Shared by the
// detail view's Install button and the Browse preview's download action so the
// two surfaces never disagree about what the same backend run is doing.
export function installPhaseLabel(phase) {
  switch (phase) {
    case 'selecting-link': return 'Finding link…'
    case 'downloading':    return 'Downloading…'
    case 'extracting':     return 'Extracting…'
    case 'installing':     return 'Installing…'
    case 'updating-db':    return 'Finalizing…'
    default:               return 'Working…'
  }
}
