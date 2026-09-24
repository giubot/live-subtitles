// SPDX-License-Identifier: Apache-2.0
import createFetchClient from 'openapi-fetch'
import { describe, expect, it } from 'vitest'
import { errorBodyMiddleware, errorCode } from './client'
import type { paths } from './schema'

function clientReturning(res: Response) {
  const c = createFetchClient<paths>({ baseUrl: 'http://test/', fetch: async () => res.clone() })
  c.use(errorBodyMiddleware)
  return c
}

describe('errorBodyMiddleware', () => {
  it.each([
    [
      'API error',
      new Response('{"code":"session.not_found","message":"x"}', {
        status: 404,
        headers: { 'Content-Type': 'application/json' },
      }),
      'session.not_found',
      undefined,
    ],
    [
      'proxy error page',
      new Response('<h1>Bad gateway</h1>', {
        status: 502,
        headers: { 'Content-Type': 'text/html' },
      }),
      'network.unreachable',
      502,
    ],
    ['empty 500', new Response(null, { status: 500 }), 'network.unreachable', 500],
    ['unknown 4xx', new Response('nope', { status: 418 }), 'request.failed', 418],
  ])('%s', async (_, res, code, status) => {
    const { error } = await clientReturning(res).GET('/api/sessions')
    expect(error).toMatchObject({ code })
    if (status) expect(error).toMatchObject({ params: { status } })
  })

  it('leaves successful responses alone', async () => {
    const { data, error } = await clientReturning(
      new Response('[]', { status: 200, headers: { 'Content-Type': 'application/json' } }),
    ).GET('/api/sessions')
    expect(error).toBeUndefined()
    expect(data).toEqual([])
  })
})

describe('errorCode', () => {
  it.each([
    [{ code: 'auth.required', message: 'x' }, 'auth.required'],
    [new TypeError('Failed to fetch'), 'network.unreachable'],
    [new Error('boom'), undefined],
  ])('%s → %s', (err, want) => {
    expect(errorCode(err)).toBe(want)
  })
})
