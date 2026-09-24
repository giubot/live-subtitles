// SPDX-License-Identifier: Apache-2.0
import type { QueryClient } from '@tanstack/react-query'
import { createRootRouteWithContext, Outlet } from '@tanstack/react-router'
import { NotFound } from '../components/NotFound'

export interface RouterContext {
  queryClient: QueryClient
}

// No theme here: themed surfaces sit under the _themed layout, while the
// stage screen and the overlay style themselves (UI-7).
export const Route = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFound,
})
