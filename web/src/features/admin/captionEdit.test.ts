// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest'
import type { Caption } from '../../api/types'
import { clockTime, correctionsUnavailable, editorLines, isNotImplemented } from './captionEdit'

function cap(segmentId: string, start: number, text = segmentId): Caption {
  return {
    sessionId: 'main',
    lang: 'es',
    segmentId,
    final: true,
    text,
    start,
    end: start + 1,
    sourceLang: 'en',
    edited: false,
    hidden: false,
  }
}

describe('clockTime', () => {
  it.each([
    [0, '0:00'],
    [5, '0:05'],
    [59.9, '0:59'],
    [60, '1:00'],
    [754.2, '12:34'],
    [3599, '59:59'],
    [3600, '1:00:00'],
    [3661, '1:01:01'],
    [36000 + 9 * 60 + 7, '10:09:07'],
    [-1, '0:00'],
    [-3700, '0:00'],
  ])('clockTime(%d) = %s', (sec, want) => expect(clockTime(sec)).toBe(want))
})

describe('editorLines', () => {
  it.each<{ name: string; finals: Caption[]; hidden: Record<string, Caption>; want: string[] }>([
    { name: 'nothing', finals: [], hidden: {}, want: [] },
    {
      name: 'finals newest first',
      finals: [cap('a', 0), cap('b', 2), cap('c', 4)],
      hidden: {},
      want: ['c', 'b', 'a'],
    },
    {
      name: 'hidden lines merged in start order',
      finals: [cap('a', 0), cap('c', 4)],
      hidden: { b: { ...cap('b', 2), hidden: true } },
      want: ['c', 'b*', 'a'],
    },
    {
      name: 'a hidden line still in the finals shows once, as hidden',
      finals: [cap('a', 0), cap('b', 2)],
      hidden: { b: { ...cap('b', 2), hidden: true } },
      want: ['b*', 'a'],
    },
    {
      name: 'only hidden lines',
      finals: [],
      hidden: { x: cap('x', 1), y: cap('y', 9) },
      want: ['y*', 'x*'],
    },
  ])('$name', ({ finals, hidden, want }) => {
    const got = editorLines(finals, hidden).map((l) => l.caption.segmentId + (l.hidden ? '*' : ''))
    expect(got).toEqual(want)
  })

  it('does not reorder the finals it was given', () => {
    const finals = [cap('a', 0), cap('b', 2)]
    editorLines(finals, {})
    expect(finals.map((c) => c.segmentId)).toEqual(['a', 'b'])
  })
})

describe('isNotImplemented', () => {
  it.each<[string, unknown, boolean]>([
    ['the server’s 501 body', { code: 'not_implemented', message: 'not implemented yet' }, true],
    ['a 501 without an API body', { code: 'network.unreachable', params: { status: 501 } }, true],
    ['a 404', { code: 'session.not_found', message: 'no such session' }, false],
    ['a 502 from a proxy', { code: 'network.unreachable', params: { status: 502 } }, false],
    ['a status passed as text', { code: 'request.failed', params: { status: '501' } }, false],
    ['a network failure', new TypeError('Failed to fetch'), false],
    ['nothing', undefined, false],
    ['a string', 'not_implemented', false],
  ])('%s → %s', (_, err, want) => expect(isNotImplemented(err)).toBe(want))
})

describe('correctionsUnavailable', () => {
  it('remembers sessions per id', () => {
    expect(correctionsUnavailable.has('helpers-a')).toBe(false)
    correctionsUnavailable.add('helpers-a')
    expect(correctionsUnavailable.has('helpers-a')).toBe(true)
    expect(correctionsUnavailable.has('helpers-b')).toBe(false)
  })
})
