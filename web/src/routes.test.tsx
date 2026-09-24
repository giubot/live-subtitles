// SPDX-License-Identifier: Apache-2.0
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import i18n from './i18n'
import { routeTree } from './routeTree.gen'

function renderAt(path: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
    context: { queryClient },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return router
}

describe('stub routes', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  it.each([
    ['/setup', 'Set up Live Subtitles'],
    ['/admin', 'Sessions'],
    ['/capture/main', 'main'],
    ['/s', 'Live sessions'],
    ['/s/main', 'Live captions · main'],
    ['/replay/main', 'Replay · main'],
    ['/nope', 'Page not found'],
  ])('%s renders "%s"', async (path, heading) => {
    renderAt(path)
    expect(await screen.findByRole('heading', { level: 1, name: heading })).toBeInTheDocument()
  })

  it('/ redirects to /admin', async () => {
    const router = renderAt('/')
    await screen.findByRole('heading', { name: 'Sessions' })
    expect(router.state.location.pathname).toBe('/admin')
  })

  it('/stage/$id renders outside the UI theme', async () => {
    renderAt('/stage/main')
    expect(await screen.findByText('Waiting for captions…')).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Interface language' })).not.toBeInTheDocument()
  })

  it('/overlay/$id renders no chrome', async () => {
    renderAt('/overlay/main')
    await waitFor(() => expect(document.querySelector('[data-overlay]')).toBeInTheDocument())
    expect(screen.queryByRole('heading')).not.toBeInTheDocument()
    expect(document.querySelector('[class*="Mui"]')).not.toBeInTheDocument()
  })

  it('Spanish UI', async () => {
    await i18n.changeLanguage('es')
    renderAt('/s/main')
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Subtítulos en vivo · main' }),
    ).toBeInTheDocument()
  })
})
