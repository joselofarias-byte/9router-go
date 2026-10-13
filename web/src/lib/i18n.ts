export const SUPPORTED_LOCALES = ['en', 'es', 'de', 'ja', 'ko', 'zh-CN', 'zh-TW'] as const
export type SupportedLocale = (typeof SUPPORTED_LOCALES)[number]

export const DEFAULT_LOCALE: SupportedLocale = 'en'
export const LANGUAGE_STORAGE_KEY = '9router_language'

type TranslationMap = Record<string, string>

let currentLocale: SupportedLocale = DEFAULT_LOCALE
let translationMap: TranslationMap = {}
let observer: MutationObserver | null = null
let localeGeneration = 0

const originalText = new WeakMap<Text, string>()
const translatedText = new WeakMap<Text, string>()
const originalAttrs = new WeakMap<Element, Map<string, string>>()
const translatedAttrs = new WeakMap<Element, Map<string, string>>()

const TRANSLATABLE_ATTRIBUTES = ['placeholder', 'title', 'aria-label'] as const
const SKIP_TAGS = new Set([
  'script',
  'style',
  'code',
  'pre',
  'colgroup',
  'table',
  'thead',
  'tbody',
  'tfoot',
  'tr',
  'select',
  'datalist',
  'optgroup',
])

export function normalizeLocale(locale: unknown): SupportedLocale {
  if (typeof locale !== 'string') return DEFAULT_LOCALE
  const clean = locale.trim()
  if (clean === 'zh') return 'zh-CN'
  return (SUPPORTED_LOCALES as readonly string[]).includes(clean)
    ? (clean as SupportedLocale)
    : DEFAULT_LOCALE
}

export function getCurrentLocale(): SupportedLocale {
  return currentLocale
}

export function translate(text: string): string {
  if (!text || currentLocale === DEFAULT_LOCALE) return text
  const trimmed = text.trim()
  if (!trimmed) return text
  return translationMap[trimmed] || text
}

function isSkipped(element: Element | null): boolean {
  let current = element
  while (current) {
    if (current.hasAttribute('data-i18n-skip')) return true
    current = current.parentElement
  }
  return false
}

function processTextNode(node: Text) {
  const parent = node.parentElement
  if (!parent || isSkipped(parent) || SKIP_TAGS.has(parent.tagName.toLowerCase())) return

  const current = node.nodeValue || ''
  if (!current.trim()) return

  const previousTranslation = translatedText.get(node)
  let original = originalText.get(node)

  // Svelte may reuse the same text node with fresh source text. If the current
  // value is neither our last translation nor the stored original, capture it.
  if (original == null || (current !== previousTranslation && current !== original)) {
    original = current
    originalText.set(node, original)
  }

  const translated = translate(original)
  translatedText.set(node, translated)
  if (current !== translated) node.nodeValue = translated
}

function processAttribute(element: Element, attr: string) {
  if (!element.hasAttribute(attr) || isSkipped(element)) return
  const current = element.getAttribute(attr)
  if (current == null || !current.trim()) return

  let originals = originalAttrs.get(element)
  if (!originals) {
    originals = new Map()
    originalAttrs.set(element, originals)
  }
  let translations = translatedAttrs.get(element)
  if (!translations) {
    translations = new Map()
    translatedAttrs.set(element, translations)
  }

  const previousTranslation = translations.get(attr)
  let original = originals.get(attr)
  if (original == null || (current !== previousTranslation && current !== original)) {
    original = current
    originals.set(attr, original)
  }

  const translated = translate(original)
  translations.set(attr, translated)
  if (current !== translated) element.setAttribute(attr, translated)
}

function processElement(root: Element) {
  if (isSkipped(root)) return

  for (const attr of TRANSLATABLE_ATTRIBUTES) processAttribute(root, attr)

  const walker = document.createTreeWalker(
    root,
    NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT,
  )

  let node: Node | null
  while ((node = walker.nextNode())) {
    if (node.nodeType === Node.TEXT_NODE) {
      processTextNode(node as Text)
    } else if (node.nodeType === Node.ELEMENT_NODE) {
      const element = node as Element
      if (!isSkipped(element)) {
        for (const attr of TRANSLATABLE_ATTRIBUTES) processAttribute(element, attr)
      }
    }
  }
}

async function loadTranslations(locale: SupportedLocale): Promise<TranslationMap> {
  if (locale === DEFAULT_LOCALE) return {}
  const response = await fetch(`/i18n/literals/${locale}.json`, { cache: 'no-cache' })
  if (!response.ok) throw new Error(`Failed to load locale ${locale}: HTTP ${response.status}`)
  return await response.json() as TranslationMap
}

function applyDocumentLocale(locale: SupportedLocale) {
  document.documentElement.lang = locale
  // Every locale currently exposed by the Go dashboard is left-to-right.
  document.documentElement.dir = 'ltr'
}

export async function setRuntimeLocale(
  locale: unknown,
  options: { persist?: boolean } = {},
): Promise<SupportedLocale> {
  const next = normalizeLocale(locale)
  const generation = ++localeGeneration

  let nextMap: TranslationMap = {}
  try {
    nextMap = await loadTranslations(next)
  } catch (err) {
    console.error('Failed to load dashboard translations:', err)
    if (next !== DEFAULT_LOCALE) {
      throw err
    }
  }

  if (generation !== localeGeneration) return currentLocale

  currentLocale = next
  translationMap = nextMap

  if (typeof document !== 'undefined') {
    applyDocumentLocale(next)
    if (document.body) processElement(document.body)
  }
  if (options.persist !== false && typeof localStorage !== 'undefined') {
    localStorage.setItem(LANGUAGE_STORAGE_KEY, next)
  }

  return next
}

export async function initRuntimeI18n(): Promise<void> {
  if (typeof window === 'undefined' || typeof document === 'undefined') return

  const stored = localStorage.getItem(LANGUAGE_STORAGE_KEY)
  try {
    await setRuntimeLocale(stored || DEFAULT_LOCALE, { persist: false })
  } catch {
    await setRuntimeLocale(DEFAULT_LOCALE, { persist: false })
  }

  if (observer || !document.body) return

  observer = new MutationObserver((mutations) => {
    for (const mutation of mutations) {
      if (mutation.type === 'characterData') {
        processTextNode(mutation.target as Text)
        continue
      }

      if (mutation.type === 'attributes') {
        const element = mutation.target as Element
        if (mutation.attributeName && TRANSLATABLE_ATTRIBUTES.includes(mutation.attributeName as typeof TRANSLATABLE_ATTRIBUTES[number])) {
          processAttribute(element, mutation.attributeName)
        }
        continue
      }

      for (const node of mutation.addedNodes) {
        if (node.nodeType === Node.TEXT_NODE) {
          processTextNode(node as Text)
        } else if (node.nodeType === Node.ELEMENT_NODE) {
          processElement(node as Element)
        }
      }
    }
  })

  observer.observe(document.body, {
    childList: true,
    subtree: true,
    characterData: true,
    attributes: true,
    attributeFilter: [...TRANSLATABLE_ATTRIBUTES],
  })
}
