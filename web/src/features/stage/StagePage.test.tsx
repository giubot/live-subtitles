// SPDX-License-Identifier: Apache-2.0
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import type { Caption, PublicSession } from '../../api/types'
import i18n from '../../i18n'
import { useCaptionsStore } from '../../realtime/captions'
import { FakeWebSocket, FakeWebSocketFactory } from '../../test/fakeWebSocket'
import { StagePage } from './StagePage'
import { parseStageSearch, stageOptions, type StageSearch } from './stageOptions'

const session: PublicSession = {
  id: 'main',
  name: 'Main stage',
  state: 'live',
  languages: ['es', 'en'],
  stageStyle: { preset: 'yellow-on-black', lines: 2, dualLanguage: false, showQr: true },
}

function cap(id: string, text: string, final: boolean, lang: string, sourceLang = 'en'): Caption {
  return {
    sessionId: 'main',
    lang,
    segmentId: id,
    final,
    text,
    start: 0,
    end: 1,
    sourceLang,
    edited: false,
    hidden: false,
  }
}

function renderStage(search: StageSearch = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(
    ['get', '/api/public/sessions/{sessionId}', { params: { path: { sessionId: 'main' } } }],
    session,
  )
  queryClient.setQueryData(['get', '/api/network'], {
    interfaces: [],
    httpPort: 8080,
    viewerBaseUrl: 'http://192.168.1.20:8080',
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <StagePage sessionId="main" search={search} WebSocket={FakeWebSocketFactory} />
    </QueryClientProvider>,
  )
}

describe('StagePage', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    FakeWebSocket.reset()
    useCaptionsStore.setState({ sessions: {} })
  })

  it('shows the last lines in the session’s preset with a QR code to the viewer', () => {
    const { container } = renderStage()
    expect(container.querySelector('main')).toHaveAttribute('data-preset', 'yellow-on-black')
    expect(FakeWebSocket.last.url).toBe(`ws://${location.host}/ws/captions/main?lang=es&history=2`)
    expect(screen.getByText('Waiting for captions…')).toBeInTheDocument()
    expect(
      screen.getByRole('img', { name: 'QR code for http://192.168.1.20:8080/s/main' }),
    ).toBeInTheDocument()
    expect(screen.getByText(/192\.168\.1\.20:8080\/s\/main/)).toBeInTheDocument()

    act(() => {
      FakeWebSocket.last.accept()
      FakeWebSocket.last.receive({ type: 'state', state: 'live' })
      FakeWebSocket.last.receive({
        type: 'history',
        captions: [
          cap('a', 'Uno.', true, 'es'),
          cap('b', 'Dos.', true, 'es'),
          cap('c', 'Tres', false, 'es'),
        ],
      })
    })
    // Two lines: the last final and the sentence in progress.
    expect(screen.queryByText('Uno.')).not.toBeInTheDocument()
    expect(screen.getByText('Dos.')).toHaveAttribute('lang', 'es')
    expect(screen.getByText('Tres')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Live · Main stage')

    act(() => FakeWebSocket.last.drop())
    expect(screen.getByRole('status')).toHaveTextContent('Reconnecting…')
    expect(screen.getByText('Dos.')).toBeInTheDocument()
  })

  it('shows a second language underneath when dual', () => {
    renderStage({ dual: true, preset: 'black-on-white', qr: false })
    expect(FakeWebSocket.last.url).toContain('lang=es&lang=en')
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    act(() => {
      FakeWebSocket.last.accept()
      FakeWebSocket.last.receive({
        type: 'caption',
        caption: cap('a', 'Hola a todos.', true, 'es'),
      })
      FakeWebSocket.last.receive({
        type: 'caption',
        caption: cap('a', 'Hello everyone.', true, 'en'),
      })
    })
    expect(screen.getByText('Hola a todos.')).toBeInTheDocument()
    expect(screen.getByText('Hello everyone.')).toHaveAttribute('lang', 'en')
  })
})

describe('stage options', () => {
  it('parses link overrides and ignores junk', () => {
    expect(
      parseStageSearch({
        preset: 'black-on-white',
        dual: '1',
        lines: '4',
        qr: 'false',
        lang: 'en',
      }),
    ).toEqual({
      preset: 'black-on-white',
      dual: true,
      lines: 4,
      qr: false,
      lang: 'en',
    })
    expect(parseStageSearch({ preset: 'pink', lines: 9, dual: 'maybe' })).toEqual({})
  })

  it('falls back from the link to the session style to the defaults', () => {
    expect(stageOptions({}, undefined, ['es', 'en'])).toEqual({
      preset: 'white-on-black',
      lines: 3,
      qr: true,
      lang: 'es',
      lang2: undefined,
    })
    expect(stageOptions({ lang: 'en', dual: true }, { lines: 2 }, ['es', 'en'])).toMatchObject({
      lines: 2,
      lang: 'en',
      lang2: 'es',
    })
    expect(stageOptions({ dual: true }, undefined, ['es'])).toMatchObject({
      lang: 'es',
      lang2: 'source',
    })
    expect(stageOptions({ lang: 'fr' }, undefined, ['es'])).toMatchObject({ lang: 'es' })
  })
})
