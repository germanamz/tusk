import { describe, expect, it } from 'vitest'
import { encodeId } from './encode'

describe('encodeId', () => {
  it('encodes segments, keeps slashes', () => {
    expect(encodeId('a/b#c')).toBe('a/' + encodeURIComponent('b#c'))
  })

  it('percent-encodes a "#" in a sub-unit id instead of letting it become a fragment', () => {
    // A sub-unit id like `notes/c#S1P1` must round-trip through the path (the
    // Go {id...} wildcard + PathValue unescape it back to one string) rather
    // than truncating at the "#" the way a raw URL would.
    expect(encodeId('notes/c#S1P1')).toBe('notes/' + encodeURIComponent('c#S1P1'))
    expect(encodeId('notes/c#S1P1')).not.toContain('#')
  })

  it('percent-encodes a space but preserves real "/" separators', () => {
    expect(encodeId('notes/foo bar')).toBe('notes/foo%20bar')
  })

  it('percent-encodes a literal "%" so it does not get misread as an escape', () => {
    expect(encodeId('notes/100%')).toBe('notes/100%25')
  })

  it('encodes every segment independently across multiple slashes', () => {
    expect(encodeId('a/b/c#d')).toBe('a/b/' + encodeURIComponent('c#d'))
  })

  it('leaves an id with no special characters unchanged', () => {
    expect(encodeId('notes/plain')).toBe('notes/plain')
  })
})
