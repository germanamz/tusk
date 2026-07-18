import { describe, it, expect } from 'vitest'
import { classifyQuery } from './search'

describe('classifyQuery', () => {
  it('treats key:value as a structural filter', () => {
    expect(classifyQuery('type:ticket status:open')).toEqual({ filter: 'type:ticket status:open', q: '' })
  })

  it('treats prose as semantic', () => {
    expect(classifyQuery('how do we rotate auth keys')).toEqual({ filter: '', q: 'how do we rotate auth keys' })
  })
})
