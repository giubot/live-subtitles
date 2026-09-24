// SPDX-License-Identifier: Apache-2.0
import { QueryClient } from '@tanstack/react-query'
import createFetchClient from 'openapi-fetch'
import createQueryHooks from 'openapi-react-query'
import type { paths } from './schema'

/** Typed fetch client for api/openapi.yaml. Same origin; the admin cookie rides along. */
export const client = createFetchClient<paths>({ baseUrl: '/', credentials: 'include' })

/** TanStack Query hooks over the client: `api.useQuery('get', '/api/sessions')`. */
export const api = createQueryHooks(client)

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false },
  },
})
