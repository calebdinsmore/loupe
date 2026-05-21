import { describe, expect, it } from 'vitest'
import { appendEvent, type AgentEvent } from './events'

const ev = (type: string, text?: string): AgentEvent => ({ type, text })

describe('appendEvent', () => {
  it('appends ordinary events', () => {
    const log = appendEvent([], ev('assistant', 'hello'))
    expect(log).toEqual([{ type: 'assistant', text: 'hello' }])
  })

  it('drops a result that repeats the last assistant text', () => {
    const prev = [ev('assistant', 'all done')]
    expect(appendEvent(prev, ev('result', 'all done'))).toBe(prev)
  })

  it('ignores whitespace differences when deduping the final result', () => {
    const prev = [ev('assistant', 'all done')]
    expect(appendEvent(prev, ev('result', '  all done\n'))).toBe(prev)
  })

  it('keeps a result that carries new text', () => {
    const prev = [ev('assistant', 'working…')]
    const log = appendEvent(prev, ev('result', 'final summary'))
    expect(log.map((e) => e.text)).toEqual(['working…', 'final summary'])
  })

  it('drops an empty result', () => {
    const prev = [ev('assistant', 'working…')]
    expect(appendEvent(prev, ev('result', ''))).toBe(prev)
    expect(appendEvent(prev, ev('result'))).toBe(prev)
  })

  it('always keeps tool_use and error events', () => {
    expect(appendEvent([], ev('tool_use'))).toEqual([{ type: 'tool_use', text: undefined }])
    expect(appendEvent([], ev('error', 'boom'))).toEqual([{ type: 'error', text: 'boom' }])
  })

  it('passes ts through unchanged', () => {
    const log = appendEvent([], { type: 'text', text: 'hi', ts: 123 })
    expect(log).toEqual([{ type: 'text', text: 'hi', ts: 123 }])
  })
})
