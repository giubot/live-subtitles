// SPDX-License-Identifier: Apache-2.0
import { vi } from 'vitest'

export interface FakeCall {
  method: string
  path: string
  body: unknown
}

/** Answer for one request: [status, JSON body], or a status alone. */
export type FakeReply = [number, unknown?] | number

export type FakeRoute = (body: unknown, path: string) => FakeReply

/**
 * Stubs global fetch with a tiny API: routes are keyed "METHOD /path"
 * (exact path, no query). Unknown routes answer 404 `route.not_found`.
 * Returns the recorded calls; undo with vi.unstubAllGlobals().
 */
export function fakeApi(routes: Record<string, FakeRoute>): FakeCall[] {
  const calls: FakeCall[] = []
  vi.stubGlobal('fetch', async (input: Request | string, init?: RequestInit) => {
    const req = input instanceof Request ? input : new Request(input, init)
    const { pathname } = new URL(req.url)
    const text = await req.text()
    const body: unknown = text ? JSON.parse(text) : undefined
    calls.push({ method: req.method, path: pathname, body })
    const route = routes[`${req.method} ${pathname}`]
    const reply: FakeReply = route
      ? route(body, pathname)
      : [404, { code: 'route.not_found', message: 'not found' }]
    const [status, json] = typeof reply === 'number' ? [reply] : reply
    return new Response(json === undefined ? null : JSON.stringify(json), {
      status,
      headers: json === undefined ? {} : { 'Content-Type': 'application/json' },
    })
  })
  return calls
}
