// Up to two initials from a game title for the typographic cover
// placeholder: "The Tides of Aurel" -> "TA", "Starforge" -> "ST".
// Bracketed suffixes ("[Ch. 2]") and articles are skipped.
const SKIP = new Set(['the', 'a', 'an', 'of', 'and', '&'])

export function initials(title = '') {
  const words = title.replace(/\[[^\]]*\]|\([^)]*\)/g, ' ')
    .split(/[\s\-_:]+/)
    .map(w => w.replace(/[^\p{L}\p{N}]/gu, ''))
    .filter(Boolean)
  const main = words.filter(w => !SKIP.has(w.toLowerCase()))
  const pick = main.length ? main : words
  if (pick.length === 0) return '?'
  if (pick.length === 1) return pick[0].slice(0, 2).toUpperCase()
  return (pick[0][0] + pick[1][0]).toUpperCase()
}
