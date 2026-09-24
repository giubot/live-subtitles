// SPDX-License-Identifier: Apache-2.0
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import type { PublicSession } from '../../api/types'
import i18n from '../../i18n'
import { routeTree } from '../../routeTree.gen'

function renderList(sessions: PublicSession[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(['get', '/api/public/sessions'], sessions)
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ['/s'] }),
    context: { queryClient },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

describe('SessionListPage', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  it('lists running sessions first, linking to their viewer', async () => {
    renderList([
      { id: 'side', name: 'Side room', state: 'idle', languages: ['es'] },
      {
        id: 'main',
        name: 'Main stage',
        room: 'Sala Konex',
        state: 'live',
        languages: ['es', 'en'],
      },
    ])
    const links = await screen.findAllByRole('link')
    expect(links.map((l) => l.getAttribute('href'))).toEqual(['/s/main', '/s/side'])
    expect(within(links[0]!).getByText('Sala Konex')).toBeInTheDocument()
    expect(within(links[0]!).getByText('Live')).toBeInTheDocument()
    expect(within(links[1]!).getByText('Not started')).toBeInTheDocument()
  })

  it('explains an empty list', async () => {
    renderList([])
    expect(await screen.findByRole('heading', { name: 'No sessions yet' })).toBeInTheDocument()
  })
})
