// SPDX-License-Identifier: Apache-2.0
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Caption } from '../../api/types'
import { useCaptionsStore } from '../../realtime/captions'
import { FakeWebSocket, FakeWebSocketFactory } from '../../test/fakeWebSocket'
import { OverlayPage } from './OverlayPage'
import {
  outlineShadow,
  parseOverlaySearch,
  presetLooks,
  resolveLook,
  vh,
  type OverlaySearch,
} from './overlayStyle'

function cap(id: string, text: string, final: boolean): Caption {
  return {
    sessionId: 'main',
    lang: 'es',
    segmentId: id,
    final,
    text,
    start: 0,
    end: 1,
    sourceLang: 'en',
    edited: false,
    hidden: false,
  }
}

function renderOverlay(search: OverlaySearch) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <OverlayPage sessionId="main" search={search} WebSocket={FakeWebSocketFactory} />
    </QueryClientProvider>,
  )
}

const region = () => document.querySelector<HTMLElement>('[aria-live]')!

describe('OverlayPage', () => {
  beforeEach(() => {
    FakeWebSocket.reset()
    useCaptionsStore.setState({ sessions: {} })
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  it('shows the latest captions, then fades after silence', () => {
    renderOverlay({ lang: 'es', fadeAfter: 4000 })
    expect(FakeWebSocket.last.url).toBe(`ws://${location.host}/ws/captions/main?lang=es&history=1`)
    expect(region().style.opacity).toBe('0')

    act(() => {
      FakeWebSocket.last.accept()
      for (const [i, text] of ['Uno.', 'Dos.', 'Tres.', 'Cuatro.'].entries()) {
        FakeWebSocket.last.receive({ type: 'caption', caption: cap(`s${i}`, text, true) })
      }
      FakeWebSocket.last.receive({ type: 'caption', caption: cap('s9', 'Y ahora', false) })
    })
    expect(screen.queryByText('Uno.')).not.toBeInTheDocument()
    expect(screen.getByText('Y ahora').closest('p')).toHaveAttribute('lang', 'es')
    expect(region().style.opacity).toBe('1')

    act(() => vi.advanceTimersByTime(3999))
    expect(region().style.opacity).toBe('1')
    act(() => vi.advanceTimersByTime(1))
    expect(region().style.opacity).toBe('0')

    act(() =>
      FakeWebSocket.last.receive({ type: 'caption', caption: cap('s9', 'Y ahora sí.', true) }),
    )
    expect(region().style.opacity).toBe('1')
  })

  it('can leave out the sentence in progress', () => {
    renderOverlay({ lang: 'es', interim: false })
    act(() => {
      FakeWebSocket.last.accept()
      FakeWebSocket.last.receive({ type: 'caption', caption: cap('s1', 'Hola.', true) })
      FakeWebSocket.last.receive({ type: 'caption', caption: cap('s2', 'Qué', false) })
    })
    expect(screen.getByText('Hola.')).toBeInTheDocument()
    expect(screen.queryByText('Qué')).not.toBeInTheDocument()
  })

  it('renders no MUI and no headings', () => {
    renderOverlay({ lang: 'es' })
    expect(document.querySelector('[class*="Mui"]')).not.toBeInTheDocument()
    expect(document.querySelector('[data-overlay]')).toBeInTheDocument()
  })
})

describe('overlay style', () => {
  it('parses link overrides and drops what it can’t trust', () => {
    expect(
      parseOverlaySearch({
        preset: 'lower-third',
        fontSize: '56',
        color: 'yellow',
        background: 'transparent',
        align: 'right',
        maxLines: 3,
        fadeAfter: '0',
        interim: '0',
      }),
    ).toEqual({
      preset: 'lower-third',
      fontSize: 56,
      color: 'yellow',
      background: 'transparent',
      align: 'right',
      maxLines: 3,
      fadeAfter: 0,
      interim: false,
    })
    expect(
      parseOverlaySearch({
        preset: 'neon',
        fontSize: 'big',
        maxLines: 9,
        color: 'url(x)"',
        align: 'justify',
      }),
    ).toEqual({})
  })

  it('layers the overrides on the preset', () => {
    expect(resolveLook({})).toEqual(presetLooks.classic)
    expect(resolveLook({ preset: 'outline', maxLines: 1 })).toMatchObject({
      background: 'transparent',
      outlineWidthPx: 3,
      maxLines: 1,
    })
  })

  it('scales px at 1080p with the source height', () => {
    expect(vh(54)).toBe('5vh')
    expect(outlineShadow(0, 'black')).toBeUndefined()
    expect(outlineShadow(3, 'black')?.split(', calc(')).toHaveLength(8)
  })
})
