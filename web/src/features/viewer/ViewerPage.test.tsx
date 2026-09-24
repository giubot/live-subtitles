// SPDX-License-Identifier: Apache-2.0
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import type { Caption, PublicSession } from '../../api/types'
import i18n from '../../i18n'
import { useCaptionsStore } from '../../realtime/captions'
import { FakeWebSocket, FakeWebSocketFactory } from '../../test/fakeWebSocket'
import { timecode } from './format'
import { langStorageKey, pickTrack, sizeStorageKey } from './prefs'
import { ViewerPage } from './ViewerPage'

const session: PublicSession = {
  id: 'main',
  name: 'Main stage',
  state: 'live',
  languages: ['es', 'en'],
  recording: true,
}

function cap(
  segmentId: string,
  text: string,
  final: boolean,
  start: number,
  lang: string,
): Caption {
  return {
    sessionId: 'main',
    lang,
    segmentId,
    final,
    text,
    start,
    end: start + 1,
    sourceLang: 'en',
    edited: false,
    hidden: false,
  }
}

function renderViewer(lang?: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(
    ['get', '/api/public/sessions/{sessionId}', { params: { path: { sessionId: 'main' } } }],
    session,
  )
  return render(
    <QueryClientProvider client={queryClient}>
      <ViewerPage sessionId="main" lang={lang} WebSocket={FakeWebSocketFactory} />
    </QueryClientProvider>,
  )
}

describe('ViewerPage', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    FakeWebSocket.reset()
    localStorage.clear()
    useCaptionsStore.setState({ sessions: {} })
  })

  it('follows the track in the UI language and shows finals and the interim', async () => {
    const user = userEvent.setup()
    renderViewer()
    expect(screen.getByRole('heading', { level: 1, name: 'Main stage' })).toBeInTheDocument()
    expect(screen.getByText('This session is recorded')).toBeInTheDocument()
    const ws = FakeWebSocket.last
    expect(ws.url).toBe(`ws://${location.host}/ws/captions/main?lang=en`)

    act(() => {
      ws.accept()
      ws.receive({ type: 'state', state: 'live' })
      ws.receive({
        type: 'history',
        captions: [
          cap('s1', 'Welcome, everyone.', true, 2, 'en'),
          cap('s2', 'Today we', false, 5, 'en'),
        ],
      })
      ws.receive({ type: 'caption', caption: cap('s0', 'Good morning.', true, 0, 'en') })
    })
    const log = screen.getByRole('log')
    expect(
      within(log)
        .getAllByText(/./, { selector: 'p' })
        .map((p) => p.textContent),
    ).toEqual(['Good morning.', 'Welcome, everyone.'])
    expect(within(log).getByText('00:00:02')).toBeInTheDocument()
    const interim = screen.getByText('Today we')
    expect(interim).toHaveAttribute('lang', 'en')
    expect(interim.parentElement).toHaveAttribute('aria-hidden', 'true')
    expect(log).not.toContainElement(interim)
    expect(screen.getByText('Live')).toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Caption language'), 'es')
    expect(FakeWebSocket.last.url).toBe(`ws://${location.host}/ws/captions/main?lang=es`)
    expect(ws.closedWith).toBe(1000)
    expect(localStorage.getItem(langStorageKey)).toBe('es')

    await user.click(screen.getByRole('button', { name: 'Larger text' }))
    expect(localStorage.getItem(sizeStorageKey)).toBe('2')

    act(() => FakeWebSocket.last.drop())
    expect(screen.getByRole('status')).toHaveTextContent('Reconnecting')
  })

  it('shows the original track with each line’s spoken language', () => {
    renderViewer('source')
    const ws = FakeWebSocket.last
    expect(ws.url).toContain('lang=source')
    act(() => {
      ws.accept()
      ws.receive({
        type: 'caption',
        caption: { ...cap('s1', 'Hola.', true, 0, 'source'), sourceLang: 'es' },
      })
    })
    expect(screen.getByText('Hola.')).toHaveAttribute('lang', 'es')
  })

  it('offers "jump to live" after scrolling back', async () => {
    renderViewer()
    act(() => {
      FakeWebSocket.last.accept()
      FakeWebSocket.last.receive({ type: 'caption', caption: cap('s1', 'One.', true, 0, 'en') })
    })
    const scroller = screen.getByLabelText('Transcript')
    Object.defineProperty(scroller, 'scrollHeight', { value: 1000, configurable: true })
    Object.defineProperty(scroller, 'clientHeight', { value: 300, configurable: true })
    expect(screen.queryByRole('button', { name: 'Jump to live' })).not.toBeInTheDocument()

    scroller.scrollTop = 100
    fireEvent.scroll(scroller)
    act(() =>
      FakeWebSocket.last.receive({ type: 'caption', caption: cap('s2', 'Two.', true, 1, 'en') }),
    )
    expect(scroller.scrollTop).toBe(100) // reading back: left alone

    await userEvent.click(screen.getByRole('button', { name: 'Jump to live' }))
    expect(scroller.scrollTop).toBe(1000)
    expect(screen.queryByRole('button', { name: 'Jump to live' })).not.toBeInTheDocument()
  })

  it('says when nothing has been said yet', () => {
    renderViewer()
    expect(screen.getByText(/Listening…/)).toBeInTheDocument()
  })
})

describe('pickTrack', () => {
  it.each([
    [{ requested: 'en', stored: 'es' }, 'en'],
    [{ requested: 'fr', stored: 'es' }, 'es'],
    [{ requested: 'source' }, 'source'],
    [{ uiLanguage: 'en' }, 'en'],
    [{ uiLanguage: 'de' }, 'es'],
  ])('%o → %s', (opts, want) => {
    expect(pickTrack(['es', 'en'], opts)).toBe(want)
  })

  it('has nothing to pick without languages', () => {
    expect(pickTrack([], { uiLanguage: 'en' })).toBeUndefined()
  })
})

describe('timecode', () => {
  it.each([
    [0, '00:00:00'],
    [59.9, '00:00:59'],
    [3725, '01:02:05'],
    [-3, '00:00:00'],
  ])('%s → %s', (s, want) => expect(timecode(s)).toBe(want))
})
