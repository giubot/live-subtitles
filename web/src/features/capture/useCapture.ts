// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useRef, useState } from 'react'
import type { ApiErrorBody, IngestServerMessage } from '../../api/types'
import { createIngestSocket, type IngestSocket, type IngestStatus } from '../../realtime/ingest'
import type { WebSocketFactory } from '../../realtime/socket'
import {
  CaptureError,
  listInputs,
  openMicrophone,
  type AudioInput,
  type Microphone,
} from './microphone'
import { levelDbfs } from './pcm'

/** Where the chosen input is remembered on this computer (AUD-2). */
export const deviceStorageKey = 'ls.capture.device'

/** `off` before Start and after Stop; `opening` while asking for the input. */
export type CaptureStatus = 'off' | 'opening' | IngestStatus

/** Frames averaged per level reading: 5 × 20 ms, ten updates a second. */
const levelFrames = 5

function readDevice(): string | undefined {
  try {
    return localStorage.getItem(deviceStorageKey) ?? undefined
  } catch {
    return undefined
  }
}

function storeDevice(id: string) {
  try {
    localStorage.setItem(deviceStorageKey, id)
  } catch {
    // Private mode: the choice lasts for this visit only.
  }
}

export interface UseCaptureOptions {
  sessionId: string
  token: string
  WebSocket?: WebSocketFactory
}

export interface Capture {
  status: CaptureStatus
  /** Local input level in dBFS, while capturing. */
  level: number
  /** The server's view of the audio: level, silence, clipping (AUD-6). */
  server?: IngestServerMessage
  error?: ApiErrorBody
  devices: AudioInput[]
  deviceId?: string
  setDevice(id: string): void
  start(): Promise<void>
  stop(): void
}

/**
 * The capture page's engine: an audio input feeding /ws/ingest. The socket
 * reconnects and buffers on its own; switching inputs keeps the connection.
 */
export function useCapture({ sessionId, token, WebSocket }: UseCaptureOptions): Capture {
  const [status, setStatus] = useState<CaptureStatus>('off')
  const [level, setLevel] = useState(-Infinity)
  const [server, setServer] = useState<IngestServerMessage>()
  const [error, setError] = useState<ApiErrorBody>()
  const [devices, setDevices] = useState<AudioInput[]>([])
  const [deviceId, setDeviceId] = useState(readDevice)
  const mic = useRef<Microphone | undefined>(undefined)
  const socket = useRef<IngestSocket | undefined>(undefined)
  const meter = useRef({ sum: 0, frames: 0 })
  // Bumped on every stop, so a slow openMicrophone knows it was cancelled.
  const generation = useRef(0)

  const refreshDevices = useCallback(() => {
    listInputs().then(setDevices, () => {
      // enumerateDevices can't fail in practice; keep the old list.
    })
  }, [])

  const stop = useCallback(() => {
    generation.current++
    mic.current?.stop()
    mic.current = undefined
    socket.current?.close()
    socket.current = undefined
    setStatus('off')
    setLevel(-Infinity)
    setServer(undefined)
  }, [])

  const onFrame = useCallback((frame: Int16Array) => {
    socket.current?.send(frame)
    const m = meter.current
    const db = levelDbfs(frame)
    m.sum += Number.isFinite(db) ? 10 ** (db / 10) : 0
    if (++m.frames >= levelFrames) {
      setLevel(m.sum > 0 ? 10 * Math.log10(m.sum / m.frames) : -Infinity)
      m.sum = 0
      m.frames = 0
    }
  }, [])

  const openInput = useCallback(
    async (id: string | undefined, gen: number) => {
      const opened = await openMicrophone({
        deviceId: id,
        onFrame,
        onEnded: () => {
          if (gen !== generation.current) return
          setError({ code: 'capture.device_lost', message: 'the audio input went away' })
          stop()
        },
      })
      if (gen !== generation.current) {
        opened.stop()
        return undefined
      }
      mic.current?.stop()
      mic.current = opened
      if (opened.deviceId) {
        setDeviceId(opened.deviceId)
        storeDevice(opened.deviceId)
      }
      refreshDevices() // labels are readable now
      return opened
    },
    [onFrame, refreshDevices, stop],
  )

  const start = useCallback(async () => {
    if (mic.current || socket.current) return
    const gen = ++generation.current
    setError(undefined)
    setStatus('opening')
    let opened: Microphone | undefined
    try {
      opened = await openInput(deviceId, gen)
    } catch (err) {
      if (gen !== generation.current) return
      setError(
        err instanceof CaptureError
          ? { code: err.code, message: err.message }
          : { code: 'capture.failed', message: String(err) },
      )
      setStatus('off')
      return
    }
    if (!opened) return
    socket.current = createIngestSocket({
      sessionId,
      token,
      deviceLabel: opened.label || undefined,
      onStatus: (s) => {
        if (gen !== generation.current) return
        setStatus(s)
        if (s === 'replaced') {
          mic.current?.stop()
          mic.current = undefined
          setLevel(-Infinity)
        }
      },
      onLevel: (l) => gen === generation.current && setServer(l),
      onError: (e) => gen === generation.current && setError(e),
      WebSocket,
    })
    setStatus(socket.current.status)
  }, [WebSocket, deviceId, openInput, sessionId, token])

  const setDevice = useCallback(
    (id: string) => {
      setDeviceId(id)
      storeDevice(id)
      if (!mic.current) return
      const gen = generation.current
      openInput(id, gen).catch((err: unknown) => {
        if (gen !== generation.current) return
        setError(
          err instanceof CaptureError
            ? { code: err.code, message: err.message }
            : { code: 'capture.failed', message: String(err) },
        )
      })
    },
    [openInput],
  )

  useEffect(() => {
    refreshDevices()
    const md = navigator.mediaDevices
    md?.addEventListener?.('devicechange', refreshDevices)
    return () => md?.removeEventListener?.('devicechange', refreshDevices)
  }, [refreshDevices])

  useEffect(() => stop, [stop])

  return { status, level, server, error, devices, deviceId, setDevice, start, stop }
}
