// Dev-only vite config: same as vite.config.js plus a plugin that serves
// generated placeholder cover PNGs at /mock/covers/<id>/thumb|full — the URL
// the mock GetCoverBaseURL returns. Never used by the Wails production build.
import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import {deflateSync} from 'node:zlib'

// ── Minimal PNG encoder (8-bit RGB, filter 0) ────────────────
const CRC_TABLE = (() => {
  const t = new Int32Array(256)
  for (let n = 0; n < 256; n++) {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    t[n] = c
  }
  return t
})()

function crc32(buf) {
  let c = 0xffffffff
  for (let i = 0; i < buf.length; i++) c = CRC_TABLE[(c ^ buf[i]) & 0xff] ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}

function chunk(type, data) {
  const len = Buffer.alloc(4)
  len.writeUInt32BE(data.length, 0)
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data])
  const crc = Buffer.alloc(4)
  crc.writeUInt32BE(crc32(body), 0)
  return Buffer.concat([len, body, crc])
}

function encodePNG(width, height, pixelFn) {
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(width, 0)
  ihdr.writeUInt32BE(height, 4)
  ihdr[8] = 8   // bit depth
  ihdr[9] = 2   // color type: truecolor RGB
  const raw = Buffer.alloc(height * (1 + width * 3))
  let o = 0
  for (let y = 0; y < height; y++) {
    raw[o++] = 0 // filter: none
    for (let x = 0; x < width; x++) {
      const [r, g, b] = pixelFn(x, y)
      raw[o++] = r
      raw[o++] = g
      raw[o++] = b
    }
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(raw)),
    chunk('IEND', Buffer.alloc(0)),
  ])
}

// Deterministic pleasant cover: pick a hue from the game id, draw a banner
// gradient with a darker base band (roughly "game key art" shaped).
const hueFor = (id) => (id * 47) % 360
const clamp = (v) => Math.max(0, Math.min(255, Math.round(v)))
const hslToRgb = (h, s, l) => {
  s /= 100; l /= 100
  const k = (n) => (n + h / 30) % 12
  const a = s * Math.min(l, 1 - l)
  const f = (n) => l - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)))
  return [f(0) * 255, f(8) * 255, f(4) * 255].map(clamp)
}

function coverPixel(id, w, h) {
  const hue = hueFor(id)
  return (x, y) => {
    const t = y / h
    const band = t < 0.78 ? 0 : 0.12 * Math.sin(x * 0.08) // bottom band
    const light = 34 - t * 16 - band
    return hslToRgb(hue, 55, light)
  }
}

// ── Vite plugin: serve generated covers ──────────────────────
function mockCovers() {
  return {
    name: 'mock-covers',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const pathname = req.url.split('?')[0]
        const m = pathname.match(/^\/mock\/covers\/(?:cover\/)?(\d+)\/(thumb|full)$/)
        if (!m) return next()
        const id = parseInt(m[1], 10) || 1
        const [w, h] = m[2] === 'full' ? [320, 180] : [72, 40]
        res.setHeader('Content-Type', 'image/png')
        res.setHeader('Cache-Control', 'no-store')
        res.end(encodePNG(w, h, coverPixel(id, w, h)))
      })
    },
  }
}

export default defineConfig({
  plugins: [svelte(), mockCovers()],
  server: {
    fs: {allow: ['../..']},
  },
})