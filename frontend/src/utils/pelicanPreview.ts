import { authorizePelicanInlineScripts, extractPelicanHtml } from './pelicanHtml'

export interface PelicanArtworkSize { width: number; height: number }
export type PelicanPreviewMode = 'fit' | 'actual'

const DEFAULT_VIEWPORT: PelicanArtworkSize = { width: 1024, height: 768 }
const MAX_DIMENSION = 8192

function validDimension(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 && value <= MAX_DIMENSION
}

export function getPelicanViewport(html: string): PelicanArtworkSize {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  const content = Array.from(doc.body.children).filter((node) => !['SCRIPT', 'STYLE', 'LINK', 'META'].includes(node.tagName))
  const svg = content.length === 1 && content[0].tagName.toLowerCase() === 'svg' ? content[0] : null
  if (!svg || Array.from(doc.body.childNodes).some((node) => node.nodeType === 3 && node.textContent?.trim())) return { ...DEFAULT_VIEWPORT }
  const pixelDimension = (value: string | null) => value && /^\s*\d+(?:\.\d+)?(?:px)?\s*$/i.test(value) ? parseFloat(value) : NaN
  const viewBox = svg.getAttribute('viewBox')?.trim().split(/[\s,]+/).map(Number)
  const boxWidth = viewBox?.length === 4 ? viewBox[2] : NaN
  const boxHeight = viewBox?.length === 4 ? viewBox[3] : NaN
  let width = pixelDimension(svg.getAttribute('width'))
  let height = pixelDimension(svg.getAttribute('height'))
  if (validDimension(boxWidth) && validDimension(boxHeight)) {
    if (validDimension(width) && !validDimension(height)) height = width * boxHeight / boxWidth
    else if (validDimension(height) && !validDimension(width)) width = height * boxWidth / boxHeight
    else if (!validDimension(width) && !validDimension(height)) { width = boxWidth; height = boxHeight }
  }
  return validDimension(width) && validDimension(height) ? { width, height } : { ...DEFAULT_VIEWPORT }
}

export function fitPelicanArtwork(artwork: PelicanArtworkSize, available: PelicanArtworkSize, mode: PelicanPreviewMode) {
  const scale = mode === 'actual' ? 1 : Math.max(0, Math.min(1, available.width / artwork.width, available.height / artwork.height))
  return { scale, width: artwork.width * scale, height: artwork.height * scale }
}

export function createPelicanPreviewChannel(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return `pelican-${Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')}`
}

export function readPelicanSizeMessage(event: MessageEvent, source: Window | null, channel: string): PelicanArtworkSize | null {
  if (!source || event.source !== source || !event.data || typeof event.data !== 'object') return null
  const data = event.data as Record<string, unknown>
  if (data.type !== 'pelican-preview:size' || data.channel !== channel || !validDimension(data.width) || !validDimension(data.height)) return null
  return { width: Math.ceil(data.width), height: Math.ceil(data.height) }
}

export function installPelicanMeasurement(target: Window & typeof globalThis, channel: string, viewport: PelicanArtworkSize, limit: number) {
  const doc = target.document
  let disposed = false
  let timer = 0
  let sent = 0
  let lastWidth = 0
  let lastHeight = 0
  let offsetX = 0
  let offsetY = 0
  let innerScale = 1
  let authorZoom = 1
  let originalTranslate = ['0px', '0px']
  let resizeObserver: ResizeObserver | undefined
  let mutationObserver: MutationObserver | undefined
  const timers: number[] = []
  const observed = new WeakSet<Element>()

  function pinDeclaration(style: CSSStyleDeclaration) {
    for (let i = 0; i < style.length; i++) {
      const property = style[i]
      const value = style.getPropertyValue(property)
      const pinned = value.replace(/("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|url\([^)]*\))|(-?\d*\.?\d+)([sld]?v[wh]|vmin|vmax)\b/gi, (match, literal: string | undefined, amount: string, unit: string) => {
        if (literal) return match
        const lower = unit.toLowerCase()
        const base = lower === 'vmin' ? Math.min(viewport.width, viewport.height)
          : lower === 'vmax' ? Math.max(viewport.width, viewport.height)
            : lower.endsWith('w') ? viewport.width : viewport.height
        return `${Number(amount) * base / 100}px`
      })
      if (pinned !== value) style.setProperty(property, pinned, style.getPropertyPriority(property))
    }
  }

  function pinRules(rules: CSSRuleList) {
    for (const rule of Array.from(rules)) {
      if ('style' in rule) pinDeclaration((rule as CSSStyleRule).style)
      if ('cssRules' in rule) pinRules((rule as CSSGroupingRule).cssRules)
    }
  }

  function prepareElements() {
    for (const sheet of Array.from(doc.styleSheets)) {
      try { pinRules(sheet.cssRules) } catch { /* An authored stylesheet may be unavailable under CSP. */ }
    }
    const elements = Array.from(doc.querySelectorAll<HTMLElement | SVGElement>('html, body, body *')).slice(0, 2000)
    for (const element of elements) {
      if (element.style) pinDeclaration(element.style)
      if (resizeObserver && !observed.has(element)) { resizeObserver.observe(element); observed.add(element) }
    }
    return elements
  }

  function measure() {
    timer = 0
    if (disposed || !doc.body || sent >= 120) return
    const elements = prepareElements()
    const root = doc.documentElement
    let width = Math.max(viewport.width, doc.body.scrollWidth * authorZoom)
    let height = Math.max(viewport.height, doc.body.scrollHeight * authorZoom)
    let left = 0
    let top = 0
    for (const element of elements) {
      if (['SCRIPT', 'STYLE', 'LINK', 'META'].includes(element.tagName)) continue
      const svg = element.closest('svg')
      if (svg && svg !== element) continue
      const rect = element.getBoundingClientRect()
      left = Math.min(left, (rect.left + target.scrollX) / innerScale - offsetX)
      top = Math.min(top, (rect.top + target.scrollY) / innerScale - offsetY)
      width = Math.max(width, (rect.right + target.scrollX) / innerScale - offsetX)
      height = Math.max(height, (rect.bottom + target.scrollY) / innerScale - offsetY)
    }
    const nextX = Math.min(limit, Math.max(offsetX, Math.ceil(-left)))
    const nextY = Math.min(limit, Math.max(offsetY, Math.ceil(-top)))
    if (nextX !== offsetX || nextY !== offsetY) {
      offsetX = nextX
      offsetY = nextY
      root.style.setProperty('translate', `calc(${originalTranslate[0]} + ${offsetX / authorZoom}px) calc(${originalTranslate[1]} + ${offsetY / authorZoom}px)`, 'important')
    }
    width = Math.min(limit, Math.ceil(width + offsetX))
    height = Math.min(limit, Math.ceil(height + offsetY))
    width = Math.max(lastWidth, width)
    height = Math.max(lastHeight, height)
    if (width === lastWidth && height === lastHeight) return
    lastWidth = width
    lastHeight = height
    innerScale = Math.min(1, viewport.width / width, viewport.height / height)
    root.style.zoom = String(authorZoom * innerScale)
    root.style.setProperty('zoom', root.style.zoom, 'important')
    sent++
    target.parent.postMessage({ type: 'pelican-preview:size', channel, width, height }, '*')
  }

  function schedule() {
    if (!disposed && !timer && sent < 120) timer = target.setTimeout(measure, 80)
  }

  function start() {
    if (disposed || !doc.body) return
    prepareElements()
    const root = doc.documentElement
    const rootStyle = target.getComputedStyle(root)
    const originalZoom = rootStyle.getPropertyValue('zoom') || root.style.getPropertyValue('zoom') || root.style.zoom
    const parsedZoom = parseFloat(originalZoom)
    if (Number.isFinite(parsedZoom) && parsedZoom > 0) authorZoom = originalZoom.endsWith('%') ? parsedZoom / 100 : parsedZoom
    const pixelSize = (value: string, fallback: number) => /^\d+(?:\.\d+)?px$/.test(value) && parseFloat(value) > 0 ? Math.min(limit, parseFloat(value)) : fallback
    const width = pixelSize(rootStyle.width, viewport.width)
    const height = pixelSize(rootStyle.height, viewport.height)
    originalTranslate = rootStyle.translate && rootStyle.translate !== 'none' ? rootStyle.translate.split(/\s+(?![^()]*\))/) : ['0px', '0px']
    if (!originalTranslate[1]) originalTranslate[1] = '0px'
    root.style.setProperty('width', `${width}px`, 'important')
    root.style.setProperty('height', `${height}px`, 'important')
    if (target.ResizeObserver) resizeObserver = new target.ResizeObserver(schedule)
    if (target.MutationObserver) {
      mutationObserver = new target.MutationObserver(schedule)
      mutationObserver.observe(doc.documentElement, { childList: true, subtree: true, attributes: true, characterData: true })
    }
    doc.addEventListener('load', schedule, true)
    target.addEventListener('resize', schedule)
    doc.fonts?.ready.then(schedule).catch(() => {})
    measure()
    for (const delay of [100, 500, 1500, 5000]) timers.push(target.setTimeout(schedule, delay))
  }

  function stop() {
    disposed = true
    target.clearTimeout(timer)
    timers.forEach((id) => target.clearTimeout(id))
    resizeObserver?.disconnect()
    mutationObserver?.disconnect()
    doc.removeEventListener('load', schedule, true)
    doc.removeEventListener('DOMContentLoaded', start)
    target.removeEventListener('resize', schedule)
    target.removeEventListener('pagehide', stop)
  }

  target.addEventListener('pagehide', stop, { once: true })
  if (doc.readyState === 'loading') doc.addEventListener('DOMContentLoaded', start, { once: true })
  else start()
  return stop
}

export function createPelicanPreviewDocument(html: string, channel: string, nonce = ''): string {
  const extractedHtml = extractPelicanHtml(html)
  if (!extractedHtml) return ''
  const protectedHtml = authorizePelicanInlineScripts(extractedHtml, nonce)
  const viewport = getPelicanViewport(protectedHtml)
  const safeChannel = channel.replace(/[^a-zA-Z0-9_-]/g, '')
  const nonceAttribute = /^[A-Za-z0-9+/_=-]+$/.test(nonce) ? ` nonce="${nonce}"` : ''
  const script = `<script data-pelican-preview="${safeChannel}"${nonceAttribute}>(${installPelicanMeasurement.toString()})(window,${JSON.stringify(safeChannel)},${JSON.stringify(viewport)},${MAX_DIMENSION});<\/script>`
  const endOfCsp = protectedHtml.indexOf('>', protectedHtml.indexOf('<meta')) + 1
  return protectedHtml.slice(0, endOfCsp) + script + protectedHtml.slice(endOfCsp)
}
