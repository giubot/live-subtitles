// SPDX-License-Identifier: Apache-2.0
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PublicSession } from '../../api/types'
import i18n from '../../i18n'
import { FakeWebSocket, FakeWebSocketFactory } from '../../test/fakeWebSocket'
import { CapturePage } from './CapturePage'
import { deviceStorageKey } from './useCapture'

const mic = vi.hoisted(() => ({
  supported: true,
  fail: undefined as string | undefined,
  onFrame: undefined as ((f: Int16Array) => void) | undefined,
  onEnded: undefined as (() => void) | undefined,
  opened: [] as (string | undefined)[],
  stop: undefined as unknown as () => void,
}))

vi.mock('./microphone', () => {
  class CaptureError extends Error {
    constructor(
      readonly code: string,
      message: string,
    ) {
      super(message)
    }
  }
  return {
    CaptureError,
    captureSupported: () => mic.supported,
    listInputs: async () =>
      mic.opened.length > 0
        ? [
            { deviceId: 'usb', label: 'USB mixer' },
            { deviceId: 'built-in', label: 'Built-in microphone' },
          ]
        : [{ deviceId: '', label: '' }],
    openMicrophone: async (o: {
      deviceId?: string
      onFrame: (f: Int16Array) => void
      onEnded?: () => void
    }) => {
      if (mic.fail) throw new CaptureError(mic.fail, 'nope')
      mic.opened.push(o.deviceId)
      mic.onFrame = o.onFrame
      mic.onEnded = o.onEnded
      const id = o.deviceId ?? 'usb'
      return {
        deviceId: id,
        label: id === 'usb' ? 'USB mixer' : 'Built-in microphone',
        stop: mic.stop,
      }
    },
  }
})

const session: PublicSession = {
  id: 'main',
  name: 'Main stage',
  state: 'idle',
  languages: ['es', 'en'],
}

function renderPage(props: { token?: string } = { token: 'tok' }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(
    ['get', '/api/public/sessions/{sessionId}', { params: { path: { sessionId: 'main' } } }],
    session,
  )
  return render(
    <QueryClientProvider client={queryClient}>
      <CapturePage sessionId="main" WebSocket={FakeWebSocketFactory} {...props} />
    </QueryClientProvider>,
  )
}

const frame = (v: number) => new Int16Array(320).fill(v)

describe('CapturePage', () => {
  let wakeLock: { request: ReturnType<typeof vi.fn>; released: number }

  beforeEach(async () => {
    await i18n.changeLanguage('en')
    FakeWebSocket.reset()
    localStorage.clear()
    Object.assign(mic, { supported: true, fail: undefined, opened: [], stop: vi.fn() })
    wakeLock = { request: vi.fn(), released: 0 }
    wakeLock.request.mockImplementation(async () => ({
      released: false,
      release: async () => void wakeLock.released++,
    }))
    Object.defineProperty(navigator, 'wakeLock', { value: wakeLock, configurable: true })
  })
  afterEach(() => {
    Reflect.deleteProperty(navigator, 'wakeLock')
  })

  it('sends audio, shows the level and remembers the input', async () => {
    const user = userEvent.setup()
    renderPage()
    expect(screen.getByRole('heading', { level: 1, name: 'Main stage' })).toBeInTheDocument()
    expect(screen.getByText(/The session hasn’t started/)).toBeInTheDocument()
    expect(screen.getByText('Not sending')).toBeInTheDocument()
    expect(
      screen.getByText('Input names appear once you allow microphone access.'),
    ).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Start sending' }))
    const ws = FakeWebSocket.last
    expect(ws.url).toBe(`ws://${location.host}/ws/ingest/main?token=tok`)
    expect(screen.getByText('Connecting')).toBeInTheDocument()
    act(() => {
      ws.accept()
      ws.receive({ type: 'ready' })
    })
    expect(ws.sentJSON()[0]).toMatchObject({ type: 'hello', deviceLabel: 'USB mixer' })
    expect(screen.getByText('Sending')).toBeInTheDocument()
    expect(wakeLock.request).toHaveBeenCalledWith('screen')
    expect(localStorage.getItem(deviceStorageKey)).toBe('usb')
    expect(await screen.findByRole('option', { name: 'Built-in microphone' })).toBeInTheDocument()
    expect(screen.getByText('Remembered on this computer.')).toBeInTheDocument()

    act(() => {
      for (let i = 0; i < 5; i++) mic.onFrame?.(frame(16384))
    })
    expect(ws.sent.at(-1)).toEqual(frame(16384))
    expect(screen.getByText('−6 dBFS · 16 kHz mono')).toBeInTheDocument()
    expect(screen.getByRole('meter')).toHaveAttribute('aria-valuenow', '-6')

    act(() =>
      ws.receive({ type: 'level', levelDbfs: -1, peakDbfs: 0, silent: false, clipping: true }),
    )
    expect(screen.getByRole('status')).toHaveTextContent('The input is clipping.')

    // Switching inputs keeps the connection.
    await user.selectOptions(screen.getByLabelText('Audio input'), 'built-in')
    expect(mic.opened).toEqual([undefined, 'built-in'])
    expect(mic.stop).toHaveBeenCalledTimes(1)
    expect(FakeWebSocket.instances).toHaveLength(1)
    expect(localStorage.getItem(deviceStorageKey)).toBe('built-in')

    await user.click(screen.getByRole('button', { name: 'Stop sending' }))
    expect(mic.stop).toHaveBeenCalledTimes(2)
    expect(ws.closedWith).toBe(1000)
    expect(screen.getByText('Not sending')).toBeInTheDocument()
    expect(wakeLock.released).toBe(1)
  })

  it('opens the remembered input next time', async () => {
    localStorage.setItem(deviceStorageKey, 'built-in')
    renderPage()
    await userEvent.click(screen.getByRole('button', { name: 'Start sending' }))
    expect(mic.opened).toEqual(['built-in'])
  })

  it('explains a blocked microphone', async () => {
    mic.fail = 'capture.permission_denied'
    renderPage()
    await userEvent.click(screen.getByRole('button', { name: 'Start sending' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Microphone access was blocked')
    expect(FakeWebSocket.instances).toHaveLength(0)
    expect(screen.getByRole('button', { name: 'Start sending' })).toBeEnabled()
  })

  it('stops when the input goes away', async () => {
    renderPage()
    await userEvent.click(screen.getByRole('button', { name: 'Start sending' }))
    act(() => mic.onEnded?.())
    expect(screen.getByRole('alert')).toHaveTextContent('The audio input went away')
    expect(FakeWebSocket.last.closedWith).toBe(1000)
  })

  it('offers to take over after another page replaced it', async () => {
    renderPage()
    await userEvent.click(screen.getByRole('button', { name: 'Start sending' }))
    act(() => {
      FakeWebSocket.last.accept()
      FakeWebSocket.last.receive({ type: 'ready' })
      FakeWebSocket.last.drop(4000)
    })
    expect(screen.getByText('Taken over')).toBeInTheDocument()
    expect(mic.stop).toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Send from this page again' }))
    expect(FakeWebSocket.instances).toHaveLength(2)
  })

  it('needs a token', () => {
    renderPage({})
    expect(screen.getByRole('status')).toHaveTextContent('no capture token')
    expect(screen.getByRole('button', { name: 'Start sending' })).toBeDisabled()
  })

  it('explains an insecure context', () => {
    mic.supported = false
    renderPage()
    expect(screen.getByRole('alert')).toHaveTextContent('This page can’t use the microphone')
    expect(screen.getByRole('button', { name: 'Start sending' })).toBeDisabled()
  })
})
