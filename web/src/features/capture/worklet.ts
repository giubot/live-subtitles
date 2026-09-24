// SPDX-License-Identifier: Apache-2.0
// AudioWorklet processor for the capture page. Vite bundles it on its own
// (`?worker&url` in microphone.ts), so it runs in AudioWorkletGlobalScope,
// where these globals exist but TypeScript's DOM lib doesn't declare them.
import { Downsampler, processorName } from './pcm'

declare const sampleRate: number
declare function registerProcessor(name: string, ctor: new () => AudioWorkletProcessor): void
declare class AudioWorkletProcessor {
  readonly port: MessagePort
}

class PcmProcessor extends AudioWorkletProcessor {
  private readonly pcm = new Downsampler(sampleRate, (frame) =>
    this.port.postMessage(frame, [frame.buffer]),
  )

  process(inputs: Float32Array[][]): boolean {
    const input = inputs[0]
    if (input && input.length > 0) this.pcm.push(input)
    return true
  }
}

registerProcessor(processorName, PcmProcessor)
