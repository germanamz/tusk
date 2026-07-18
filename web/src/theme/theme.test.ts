import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { cssVar, getMode, getResolvedTheme, initTheme, onThemeChange, setMode } from './theme'

// theme.ts is a module singleton, so each test resets the persisted mode and the
// stamped attribute. matchMedia is polyfilled (matches:false → light) by the
// global setup; tests that need a dark OS preference override it locally.
beforeEach(() => {
  localStorage.clear()
  document.documentElement.removeAttribute('data-theme')
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('theme controller', () => {
  it('defaults to system and resolves to light when the OS is light', () => {
    initTheme()

    expect(getMode()).toBe('system')
    expect(getResolvedTheme()).toBe('light')
    expect(document.documentElement.getAttribute('data-theme')).toBe('light')
  })

  it('setMode stamps the theme, persists the mode, and notifies subscribers', () => {
    const seen: string[] = []
    const unsubscribe = onThemeChange((theme) => seen.push(theme))

    setMode('dark')

    expect(getMode()).toBe('dark')
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    expect(localStorage.getItem('tusk.theme')).toBe('dark')
    expect(seen).toEqual(['dark'])

    unsubscribe()
    setMode('light')

    expect(seen).toEqual(['dark']) // no further callback after unsubscribe
  })

  it('initTheme restores a previously persisted mode', () => {
    localStorage.setItem('tusk.theme', 'dark')

    initTheme()

    expect(getMode()).toBe('dark')
    expect(getResolvedTheme()).toBe('dark')
  })

  it('cssVar reads a custom property off the document root', () => {
    document.documentElement.style.setProperty('--probe', '#123456')

    expect(cssVar('--probe')).toBe('#123456')
  })
})
