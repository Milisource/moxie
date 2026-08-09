/**
 * Allowlist for external URLs rendered in the Wails webview.
 *
 * The webview exposes window.go / window.runtime, so any scraped string that
 * ends up in an `href` must be strictly limited to http/https. Anything else
 * (javascript:, data:, vbscript:, file:, bare protocols, non-strings) returns
 * '' so callers can skip rendering the element entirely.
 *
 * @param {unknown} url
 * @returns {string} the original url when it is a safe http(s) URL, else ''
 */
export function safeExternalUrl(url) {
  if (typeof url !== 'string' || url.length === 0) return ''
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? url : ''
  } catch {
    // Not a parseable absolute URL (relative, protocol-relative, garbage).
    return ''
  }
}
