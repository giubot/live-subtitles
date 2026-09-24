// SPDX-License-Identifier: Apache-2.0
import { processorName } from './pcm'
import workletUrl from './worklet.ts?worker&url'

export interface AudioInput {
  deviceId: string
  /** Empty until the page has microphone permission. */
  label: string
}

export interface Microphone {
  /** The device actually opened (the saved one may be gone). */
  deviceId: string
  label: string
  stop(): void
}

export interface OpenMicrophoneOptions {
  /** Preferred input; falls back to the default one if it's gone. */
  deviceId?: string
  /** 20 ms frames of 16 kHz mono s16le. */
  onFrame: (frame: Int16Array) => void
  /** The device went away (unplugged) or the browser revoked access. */
  onEnded?: () => void
}

/** Error codes (errors.<code> in common.json) for capture failures. */
export type CaptureErrorCode =
  | 'capture.insecure'
  | 'capture.permission_denied'
  | 'capture.no_device'
  | 'capture.device_busy'
  | 'capture.failed'

export class CaptureError extends Error {
  constructor(
    readonly code: CaptureErrorCode,
    message: string,
  ) {
    super(message)
  }
}

/**
 * Whether this page may use the microphone at all: getUserMedia only
 * exists in a secure context (https, or http://localhost; TLS-2).
 */
export function captureSupported(): boolean {
  return window.isSecureContext && !!navigator.mediaDevices?.getUserMedia
}

export async function listInputs(): Promise<AudioInput[]> {
  const all = await navigator.mediaDevices.enumerateDevices()
  return all
    .filter((d) => d.kind === 'audioinput')
    .map(({ deviceId, label }) => ({ deviceId, label }))
}

// Mixers and line inputs are already clean; browser voice processing
// would pump the level and smear the audio the ASR hears.
const processing = { echoCancellation: false, noiseSuppression: false, autoGainControl: false }

async function getStream(deviceId?: string): Promise<MediaStream> {
  try {
    return await navigator.mediaDevices.getUserMedia({
      audio: deviceId ? { ...processing, deviceId: { exact: deviceId } } : processing,
    })
  } catch (err) {
    if (deviceId && err instanceof DOMException && err.name === 'OverconstrainedError') {
      return getStream() // the saved device is gone: use the default one
    }
    throw toCaptureError(err)
  }
}

function toCaptureError(err: unknown): CaptureError {
  const name = err instanceof DOMException ? err.name : ''
  const message = err instanceof Error ? err.message : String(err)
  switch (name) {
    case 'NotAllowedError':
    case 'SecurityError':
      return new CaptureError('capture.permission_denied', message)
    case 'NotFoundError':
      return new CaptureError('capture.no_device', message)
    case 'NotReadableError':
    case 'AbortError':
      return new CaptureError('capture.device_busy', message)
    default:
      return new CaptureError('capture.failed', message)
  }
}

/**
 * Opens an audio input and streams it as 16 kHz mono frames from an
 * AudioWorklet (AUD-1). The AudioContext runs at the device's own rate
 * (Firefox can't connect a stream to a context at another rate), and the
 * worklet downmixes and resamples. Call it from a click, so the context
 * may start.
 */
export async function openMicrophone(o: OpenMicrophoneOptions): Promise<Microphone> {
  if (!captureSupported()) throw new CaptureError('capture.insecure', 'not a secure context')
  const stream = await getStream(o.deviceId)
  const track = stream.getAudioTracks()[0]
  const ctx = new AudioContext()
  const stopAll = () => {
    for (const t of stream.getTracks()) t.stop()
    void ctx.close()
  }
  try {
    await ctx.audioWorklet.addModule(workletUrl)
    const source = ctx.createMediaStreamSource(stream)
    const node = new AudioWorkletNode(ctx, processorName, { numberOfOutputs: 1 })
    node.port.onmessage = (ev: MessageEvent<Int16Array>) => o.onFrame(ev.data)
    // Some browsers only run nodes that reach the destination; a muted
    // gain keeps the worklet pulled without playing the input back.
    const mute = ctx.createGain()
    mute.gain.value = 0
    source.connect(node).connect(mute).connect(ctx.destination)
    await ctx.resume()
  } catch (err) {
    stopAll()
    throw toCaptureError(err)
  }
  track?.addEventListener('ended', () => o.onEnded?.())
  return {
    deviceId: track?.getSettings().deviceId ?? o.deviceId ?? '',
    label: track?.label ?? '',
    stop: stopAll,
  }
}
