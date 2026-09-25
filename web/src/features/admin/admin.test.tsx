// SPDX-License-Identifier: Apache-2.0
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '../../i18n'
import { useAdminEventsStore, emptyAdminEvents } from '../../realtime/admin'
import { routeTree } from '../../routeTree.gen'
import { fakeApi } from '../../test/fakeApi'
import { FakeWebSocket, FakeWebSocketFactory } from '../../test/fakeWebSocket'
import { captureBase, slugify, validate, newSessionValues, type Session } from './sessionForm'

const base = 'http://192.168.1.20:8080'
function session(id: string, name: string): Session {
  return {
    id,
    name,
    room: 'Sala Konex',
    sourceLanguage: 'auto',
    targetLanguages: ['es', 'en'],
    provider: 'default',
    effectiveProvider: 'mock',
    recordingEnabled: true,
    state: 'idle',
    createdAt: '2026-09-25T13:30:00Z',
    updatedAt: '2026-09-25T13:30:00Z',
    urls: {
      viewer: `${base}/s/${id}`,
      stage: `${base}/stage/${id}`,
      overlay: `${base}/overlay/${id}?lang=es`,
      capture: `${base}/capture/${id}`,
      replay: `${base}/replay/${id}`,
    },
  }
}

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

const setupDone = {
  completed: true,
  adminPinSet: true,
  googleApiKeySet: false,
  localModelsReady: false,
}
const network = { interfaces: [], preferredIp: '192.168.1.20', httpPort: 8080, viewerBaseUrl: base }

describe('admin', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    FakeWebSocket.reset()
    vi.stubGlobal('WebSocket', FakeWebSocketFactory)
    useAdminEventsStore.setState({ ...emptyAdminEvents })
  })
  afterEach(() => vi.unstubAllGlobals())

  it('logs in, creates a session, shows its links and starts it', async () => {
    const user = userEvent.setup()
    let authed = false
    let sessions: Session[] = []
    const calls = fakeApi({
      'GET /api/auth/me': () => [200, { authenticated: authed }],
      'GET /api/setup': () => [200, setupDone],
      'POST /api/auth/login': (body) => {
        if ((body as { pin: string }).pin !== '2468') {
          return [401, { code: 'auth.invalid_pin', message: 'wrong' }]
        }
        authed = true
        return 204
      },
      'GET /api/network': () => [200, network],
      'GET /api/sessions': () => [200, sessions],
      'POST /api/sessions': (body) => {
        const s = { ...session('sala-konex', 'Sala Konex – Día 2'), ...(body as object) }
        sessions = [s]
        return [201, { ...s, ingestToken: 'tok/1' }]
      },
      'POST /api/sessions/sala-konex/start': () => [
        200,
        { sessionId: 'sala-konex', state: 'live', viewers: 0 },
      ],
    })

    renderAt('/admin')
    const pin = await screen.findByLabelText('Admin PIN')
    await user.type(pin, '0000{Enter}')
    expect(await screen.findByRole('alert')).toHaveTextContent('PIN')
    await user.clear(pin)
    await user.type(pin, '2468{Enter}')

    expect(await screen.findByRole('heading', { level: 1, name: 'Sessions' })).toBeInTheDocument()
    expect(await screen.findByText('No sessions yet')).toBeInTheDocument()
    expect(await screen.findByText('192.168.1.20:8080')).toBeInTheDocument()
    expect(FakeWebSocket.last.url).toBe(`ws://${location.host}/ws/admin`)

    await user.click(screen.getByRole('button', { name: 'New session' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Create session' }))
    expect(
      within(dialog).getByText('Give the session a name (up to 120 characters).'),
    ).toBeInTheDocument()
    await user.type(within(dialog).getByLabelText('Name'), 'Sala Konex – Día 2')
    expect(within(dialog).getByLabelText('Address')).toHaveValue('sala-konex-dia-2')
    await user.click(within(dialog).getByRole('checkbox', { name: 'English' }))
    await user.click(within(dialog).getByRole('button', { name: 'Create session' }))

    const create = calls.find((c) => c.method === 'POST' && c.path === '/api/sessions')
    expect(create?.body).toMatchObject({
      slug: 'sala-konex-dia-2',
      name: 'Sala Konex – Día 2',
      targetLanguages: ['es'],
      provider: 'default',
      recordingEnabled: true,
    })
    const card = await screen.findByRole('article', { name: 'Sala Konex – Día 2' })
    // Just created: the links are open, with the capture token on this origin.
    expect(
      within(card).getByText(`${location.origin}/capture/sala-konex?token=tok%2F1`),
    ).toBeInTheDocument()
    expect(within(card).getByText(`${base}/s/sala-konex`)).toBeInTheDocument()
    expect(
      within(card).getByRole('img', { name: `QR code for ${base}/s/sala-konex` }),
    ).toBeInTheDocument()

    await user.click(within(card).getByRole('button', { name: 'Start' }))
    expect(calls.some((c) => c.path === '/api/sessions/sala-konex/start')).toBe(true)
    act(() => {
      FakeWebSocket.last.accept()
      FakeWebSocket.last.receive({
        type: 'sessionStatus',
        at: '2026-09-25T13:30:00Z',
        status: { sessionId: 'sala-konex', state: 'live', viewers: 12, detectedLanguage: 'en' },
      })
    })
    expect(within(card).getByText('Live')).toBeInTheDocument()
    expect(within(card).getByRole('button', { name: 'Pause' })).toBeInTheDocument()
    expect(within(card).getByText('12')).toBeInTheDocument()
    expect(within(card).getByText('EN')).toBeInTheDocument()
  })

  it('sends a fresh install to /setup, then into the admin', async () => {
    const user = userEvent.setup()
    let pinSet = false
    const calls = fakeApi({
      'GET /api/auth/me': () => [200, { authenticated: pinSet }],
      'GET /api/setup': () => [200, { ...setupDone, completed: pinSet, adminPinSet: pinSet }],
      'POST /api/setup': () => {
        pinSet = true
        return 204
      },
      'GET /api/network': () => [200, network],
      'GET /api/sessions': () => [200, []],
    })
    const router = renderAt('/admin')
    const pin = await screen.findByLabelText('Admin PIN')
    expect(router.state.location.pathname).toBe('/setup')

    await user.type(pin, '12')
    await user.click(screen.getByRole('button', { name: 'Save PIN and continue' }))
    expect(screen.getByText('Use at least 4 characters.')).toBeInTheDocument()
    await user.type(pin, '34')
    await user.type(screen.getByLabelText('Repeat the PIN'), '1235')
    await user.click(screen.getByRole('button', { name: 'Save PIN and continue' }))
    expect(screen.getByText('The two PINs don’t match.')).toBeInTheDocument()
    await user.clear(screen.getByLabelText('Repeat the PIN'))
    await user.type(screen.getByLabelText('Repeat the PIN'), '1234')
    await user.click(screen.getByRole('button', { name: 'Save PIN and continue' }))
    // The wizard goes on to the hardware check; the rest can wait.
    await user.click(await screen.findByRole('link', { name: 'Finish later' }))

    expect(await screen.findByRole('heading', { level: 1, name: 'Sessions' })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/admin')
    expect(calls.find((c) => c.path === '/api/setup' && c.method === 'POST')?.body).toEqual({
      pin: '1234',
    })
  })

  it('edits a session and deletes it after confirming', async () => {
    const user = userEvent.setup()
    let sessions = [session('main', 'Main stage')]
    const calls = fakeApi({
      'GET /api/auth/me': () => [200, { authenticated: true }],
      'GET /api/network': () => [200, network],
      'GET /api/sessions': () => [200, sessions],
      'PATCH /api/sessions/main': () => [
        409,
        { code: 'session.state_conflict', message: 'running', params: { state: 'live' } },
      ],
      'DELETE /api/sessions/main': () => {
        sessions = []
        return 204
      },
    })
    renderAt('/admin')
    // Wait with a cheap text query; *ByRole is slow on a full MUI page.
    await screen.findByText('Main stage')
    const card = screen.getByRole('article', { name: 'Main stage' })
    await user.click(within(card).getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog', { name: 'Edit Main stage' })
    expect(within(dialog).getByLabelText('Address')).toBeDisabled()
    await user.click(within(dialog).getByRole('button', { name: 'Save changes' }))
    expect(await within(dialog).findByRole('alert')).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'PATCH')?.body).toMatchObject({
      name: 'Main stage',
      room: 'Sala Konex',
    })

    await user.click(within(dialog).getByRole('button', { name: 'Delete session' }))
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    expect(await screen.findByText('No sessions yet')).toBeInTheDocument()
  })
})

describe('session form helpers', () => {
  it.each([
    ['Main stage', 'main-stage'],
    ['Sala Konex – Día 2', 'sala-konex-dia-2'],
    ['  --Ñandú!!  ', 'nandu'],
    ['a'.repeat(70), 'a'.repeat(63)],
  ])('slugify(%j) = %s', (name, want) => expect(slugify(name)).toBe(want))

  it('validates before sending', () => {
    expect(validate({ ...newSessionValues, name: 'x', slug: 'ok-1' }, true)).toEqual({})
    expect(
      validate({ ...newSessionValues, name: ' ', slug: '-bad', targetLanguages: [] }, true),
    ).toEqual({
      name: 'request.invalid',
      slug: 'session.slug_invalid',
      targetLanguages: 'request.invalid',
    })
    // The slug can't change on edit, so it isn't checked.
    expect(validate({ ...newSessionValues, name: 'x', slug: '' }, false)).toEqual({})
  })

  it('keeps capture links on localhost when the admin is there', () => {
    const s = session('main', 'Main')
    expect(captureBase(s, { hostname: 'localhost', origin: 'http://localhost:18080' })).toBe(
      'http://localhost:18080/capture/main',
    )
    expect(captureBase(s, { hostname: '192.168.1.20', origin: base })).toBe(`${base}/capture/main`)
  })
})
