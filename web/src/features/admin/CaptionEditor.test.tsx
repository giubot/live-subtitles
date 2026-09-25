// SPDX-License-Identifier: Apache-2.0
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Caption } from '../../api/types'
import i18n from '../../i18n'
import { useCaptionsStore } from '../../realtime/captions'
import { fakeApi, type FakeRoute } from '../../test/fakeApi'
import { FakeWebSocket, FakeWebSocketFactory } from '../../test/fakeWebSocket'
import { CaptionEditor } from './CaptionEditor'
import type { Session } from './sessionForm'

function session(id: string): Session {
  const base = 'http://192.168.1.20:8080'
  return {
    id,
    name: 'Main stage',
    room: 'Sala Konex',
    sourceLanguage: 'en',
    targetLanguages: ['es', 'en'],
    provider: 'default',
    effectiveProvider: 'mock',
    recordingEnabled: true,
    state: 'live',
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

function cap(
  sessionId: string,
  segmentId: string,
  text: string,
  start: number,
  lang = 'es',
): Caption {
  return {
    sessionId,
    lang,
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

/** Renders the editor and plays the server's history on /ws/captions. */
function open(s: Session, history: Caption[]) {
  const onClose = vi.fn()
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={queryClient}>
      <CaptionEditor session={s} onClose={onClose} />
    </QueryClientProvider>,
  )
  act(() => {
    FakeWebSocket.last.accept()
    FakeWebSocket.last.receive({ type: 'history', captions: history })
  })
  return { onClose, unmount: view.unmount }
}

/** The list item that shows `text`. */
function row(text: string): HTMLElement {
  const li = screen.getByText(text).closest('li')
  if (!li) throw new Error(`no line with ${text}`)
  return li
}

/** Records request URLs (fakeApi keeps only the path), to check the lang query. */
function recordUrls(): string[] {
  const urls: string[] = []
  const fetch = globalThis.fetch
  vi.stubGlobal('fetch', (req: Request) => {
    urls.push(req.url)
    return fetch(req)
  })
  return urls
}

/** PATCH …/captions/{segmentId} that echoes the body onto the caption, as the server does. */
function echo(sessionId: string, captions: Caption[]): FakeRoute {
  return (body, path) => {
    const id = path.split('/').at(-1)!
    const c = captions.find((x) => x.segmentId === id)!
    const b = body as { text?: string; hidden?: boolean }
    return [200, { ...c, sessionId, ...b, edited: b.text !== undefined || c.edited }]
  }
}

describe('CaptionEditor', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    FakeWebSocket.reset()
    vi.stubGlobal('WebSocket', FakeWebSocketFactory)
    useCaptionsStore.setState({ sessions: {} })
  })
  afterEach(() => vi.unstubAllGlobals())

  it('lists the track newest first and edits a line with Enter, cancels with Esc', async () => {
    const user = userEvent.setup()
    const history = [cap('edit', 's1', 'Hola a todos.', 5), cap('edit', 's2', 'Hoy hablamos.', 65)]
    const calls = fakeApi({
      'PATCH /api/sessions/edit/captions/s1': echo('edit', history),
    })
    const urls = recordUrls()
    const { onClose } = open(session('edit'), history)

    expect(FakeWebSocket.last.url).toBe(
      `ws://${location.host}/ws/captions/edit?lang=es&history=200`,
    )
    const list = screen.getByRole('list', { name: 'Recent lines, ES' })
    const items = within(list).getAllByRole('listitem')
    expect(items.map((li) => li.textContent)).toEqual([
      expect.stringContaining('1:05Hoy hablamos.'),
      expect.stringContaining('0:05Hola a todos.'),
    ])

    // Esc cancels the edit without a request, and doesn't close the drawer.
    await user.click(within(row('Hola a todos.')).getByRole('button', { name: 'Edit line' }))
    const box = screen.getByRole('textbox', { name: 'Caption text' })
    expect(box).toHaveValue('Hola a todos.')
    await user.type(box, ' Bienvenidos')
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('textbox', { name: 'Caption text' })).not.toBeInTheDocument()
    expect(screen.getByText('Hola a todos.')).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
    expect(calls).toEqual([])

    // Enter saves the trimmed text.
    await user.click(within(row('Hola a todos.')).getByRole('button', { name: 'Edit line' }))
    const edit = screen.getByRole('textbox', { name: 'Caption text' })
    await user.clear(edit)
    await user.type(edit, '  Hola a todas.  {Enter}')
    await waitFor(() =>
      expect(screen.queryByRole('textbox', { name: 'Caption text' })).not.toBeInTheDocument(),
    )
    expect(calls).toHaveLength(1)
    expect(calls[0]).toEqual({
      method: 'PATCH',
      path: '/api/sessions/edit/captions/s1',
      body: { text: 'Hola a todas.' },
    })
    expect(new URL(urls[0]!).searchParams.getAll('lang')).toEqual(['es'])

    // The corrected line comes back over the socket.
    act(() =>
      FakeWebSocket.last.receive({
        type: 'caption',
        caption: { ...history[0]!, text: 'Hola a todas.', edited: true },
      }),
    )
    expect(within(row('Hola a todas.')).getByText('Edited')).toBeInTheDocument()
  })

  it('saves nothing when the text is unchanged or blank', async () => {
    const user = userEvent.setup()
    const history = [cap('same', 's1', 'Uno.', 0)]
    const calls = fakeApi({ 'PATCH /api/sessions/same/captions/s1': echo('same', history) })
    open(session('same'), history)

    await user.click(within(row('Uno.')).getByRole('button', { name: 'Edit line' }))
    await user.type(screen.getByRole('textbox', { name: 'Caption text' }), '{Enter}')
    expect(screen.queryByRole('textbox', { name: 'Caption text' })).not.toBeInTheDocument()

    await user.click(within(row('Uno.')).getByRole('button', { name: 'Edit line' }))
    await user.clear(screen.getByRole('textbox', { name: 'Caption text' }))
    await user.type(screen.getByRole('textbox', { name: 'Caption text' }), '   {Enter}')
    expect(screen.queryByRole('textbox', { name: 'Caption text' })).not.toBeInTheDocument()
    expect(calls).toEqual([])
  })

  it('hides a line, keeps it listed as hidden, and shows it again', async () => {
    const user = userEvent.setup()
    const history = [cap('hide', 's1', 'Uno.', 0), cap('hide', 's2', 'Dos.', 2)]
    const calls = fakeApi({ 'PATCH /api/sessions/hide/captions/s1': echo('hide', history) })
    open(session('hide'), history)

    await user.click(within(row('Uno.')).getByRole('button', { name: 'Hide line from viewers' }))
    await within(row('Uno.')).findByText('Hidden')
    expect(calls.at(-1)).toMatchObject({
      method: 'PATCH',
      path: '/api/sessions/hide/captions/s1',
      body: { hidden: true },
    })
    // The server's broadcast drops it from the live track; the editor keeps it.
    act(() =>
      FakeWebSocket.last.receive({ type: 'caption', caption: { ...history[0]!, hidden: true } }),
    )
    const line = row('Uno.')
    expect(within(line).getByText('Hidden')).toBeInTheDocument()
    expect(within(line).queryByRole('button', { name: 'Edit line' })).not.toBeInTheDocument()
    const items = within(screen.getByRole('list', { name: 'Recent lines, ES' })).getAllByRole(
      'listitem',
    )
    expect(items.map((li) => li.textContent)).toEqual([
      expect.stringContaining('Dos.'),
      expect.stringContaining('Uno.'),
    ])

    await user.click(within(line).getByRole('button', { name: 'Show line again' }))
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1]?.body).toEqual({ hidden: false })
    // The server broadcasts it again, back on the live track.
    act(() => FakeWebSocket.last.receive({ type: 'caption', caption: history[0] }))
    await waitFor(() => expect(within(row('Uno.')).queryByText('Hidden')).not.toBeInTheDocument())
    expect(
      within(row('Uno.')).getByRole('button', { name: 'Hide line from viewers' }),
    ).toBeEnabled()
  })

  it('turns the controls off when the server has no corrections (501)', async () => {
    const user = userEvent.setup()
    const history = [cap('old', 's1', 'Uno.', 0), cap('old', 's2', 'Dos.', 2)]
    const calls = fakeApi({
      'PATCH /api/sessions/old/captions/s1': () => [
        501,
        { code: 'not_implemented', message: 'not implemented yet' },
      ],
    })
    const first = open(session('old'), history)
    const unavailable = /Correction isn’t available on this server yet/
    expect(screen.queryByText(unavailable)).not.toBeInTheDocument()

    await user.click(within(row('Uno.')).getByRole('button', { name: 'Hide line from viewers' }))
    expect(await screen.findByText(unavailable)).toBeInTheDocument()
    expect(calls).toHaveLength(1)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    for (const name of ['Edit line', 'Hide line from viewers']) {
      for (const b of screen.getAllByRole('button', { name })) expect(b).toBeDisabled()
    }
    expect(within(row('Uno.')).queryByText('Hidden')).not.toBeInTheDocument()

    // Opened again, the editor remembers.
    await user.click(screen.getByRole('button', { name: 'Close' }))
    expect(first.onClose).toHaveBeenCalledOnce()
    first.unmount()
    open(session('old'), history)
    expect(screen.getByText(unavailable)).toBeInTheDocument()
    expect(within(row('Dos.')).getByRole('button', { name: 'Edit line' })).toBeDisabled()
  })

  it('shows other failures and keeps the controls on', async () => {
    const user = userEvent.setup()
    const history = [cap('fail', 's1', 'Uno.', 0)]
    fakeApi({
      'PATCH /api/sessions/fail/captions/s1': () => [
        404,
        { code: 'session.not_found', message: 'no such session' },
      ],
    })
    open(session('fail'), history)
    await user.click(within(row('Uno.')).getByRole('button', { name: 'Hide line from viewers' }))
    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(screen.queryByText(/Correction isn’t available/)).not.toBeInTheDocument()
    expect(within(row('Uno.')).getByRole('button', { name: 'Edit line' })).toBeEnabled()
  })

  it('patches the track that is selected', async () => {
    const user = userEvent.setup()
    const history = [
      cap('tracks', 's1', 'Hola.', 0, 'es'),
      cap('tracks', 's1', 'Hello.', 0, 'en'),
      { ...cap('tracks', 's1', 'Hello.', 0, 'source'), sourceLang: 'en' },
    ]
    const calls = fakeApi({
      'PATCH /api/sessions/tracks/captions/s1': (body) => [
        200,
        { ...history[1]!, ...(body as object) },
      ],
    })
    const urls = recordUrls()
    open(session('tracks'), history)
    const group = screen.getByRole('group', { name: 'Track' })
    expect(
      within(group)
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual(['Source (EN)', 'ES', 'EN'])
    expect(within(group).getByRole('button', { name: 'ES' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(screen.getByText('Hola.')).toHaveAttribute('lang', 'es')

    await user.click(within(group).getByRole('button', { name: 'EN' }))
    expect(FakeWebSocket.last.url).toBe(
      `ws://${location.host}/ws/captions/tracks?lang=en&history=200`,
    )
    act(() => {
      FakeWebSocket.last.accept()
      FakeWebSocket.last.receive({ type: 'history', captions: history })
    })
    expect(screen.getByRole('list', { name: 'Recent lines, EN' })).toBeInTheDocument()
    expect(screen.queryByText('Hola.')).not.toBeInTheDocument()
    await user.click(within(row('Hello.')).getByRole('button', { name: 'Edit line' }))
    const box = screen.getByRole('textbox', { name: 'Caption text' })
    await user.clear(box)
    await user.type(box, 'Hello, everyone.{Enter}')
    await waitFor(() => expect(calls).toHaveLength(1))

    const url = new URL(urls.at(-1)!)
    expect(url.pathname).toBe('/api/sessions/tracks/captions/s1')
    expect(url.searchParams.get('lang')).toBe('en')
    expect(calls[0]?.body).toEqual({ text: 'Hello, everyone.' })
  })
})
