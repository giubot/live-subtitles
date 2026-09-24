// SPDX-License-Identifier: Apache-2.0
import { QueryClient } from '@tanstack/react-query'
import createFetchClient, { type Middleware } from 'openapi-fetch'
import createQueryHooks from 'openapi-react-query'
import { networkErrorCode, toApiError } from '../components/apiError'
import type { paths } from './schema'
import type { ApiErrorBody } from './types'

/** Code for an error response the server didn't describe (no API Error body). */
export const unexpectedErrorCode = 'request.failed'

/**
 * Gives every failed response an API Error body (`{ code, message, params }`),
 * so `describeError` can translate it (UI-4). The Go server always sends
 * one; anything else comes from in between: the dev proxy or a reverse
 * proxy answering for a server that's down (5xx), or an unknown status.
 */
export const errorBodyMiddleware: Middleware = {
  async onResponse({ response }) {
    if (response.ok || response.headers.get('content-type')?.includes('application/json')) {
      return undefined
    }
    const body: ApiErrorBody = {
      code: response.status >= 500 ? networkErrorCode : unexpectedErrorCode,
      message: response.statusText || `HTTP ${response.status}`,
      params: { status: response.status },
    }
    return new Response(JSON.stringify(body), {
      status: response.status,
      statusText: response.statusText,
      headers: { 'Content-Type': 'application/json' },
    })
  },
}

/**
 * Typed fetch client for api/openapi.yaml. Same origin; the admin cookie
 * rides along. Failed requests resolve with `error` set to an API Error
 * body, and the query hooks throw it: pass it to `ErrorAlert`.
 */
export const client = createFetchClient<paths>({
  // The page's own origin: same as '/' in a browser, and absolute so tests
  // (Node's Request) can resolve it.
  baseUrl: `${globalThis.location?.origin ?? ''}/`,
  credentials: 'include',
  // Looked up on every call rather than captured at startup, so tests can
  // stub fetch per test.
  fetch: (request) => globalThis.fetch(request),
})
client.use(errorBodyMiddleware)

/** TanStack Query hooks over the client: `api.useQuery('get', '/api/sessions')`. */
export const api = createQueryHooks(client)

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false },
  },
})

/** The translatable code of a failed request, e.g. to redirect on `auth.required`. */
export function errorCode(err: unknown): string | undefined {
  return toApiError(err)?.code
}
