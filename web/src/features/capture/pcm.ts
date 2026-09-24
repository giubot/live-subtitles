// SPDX-License-Identifier: Apache-2.0

/** What /ws/ingest takes: 16 kHz mono s16le in 20 ms frames. */
export const targetRate = 16000
export const frameSamples = 320

/** Name the worklet registers its processor under. */
export const processorName = 'livesubs-pcm'

/**
 * Downmixes to mono and resamples to 16 kHz, emitting 20 ms s16le frames.
 *
 * Each output sample is the average of the input it covers (area
 * averaging, fractional at the edges), which is a box low-pass: enough
 * anti-aliasing for speech recognition from 44.1 or 48 kHz, and cheap
 * enough for the audio thread. Below 16 kHz it holds samples instead.
 */
export class Downsampler {
  /** Input samples per output sample. */
  private readonly step: number
  private sum = 0
  private weight = 0
  private frame = new Int16Array(frameSamples)
  private filled = 0

  constructor(
    inputRate: number,
    private readonly onFrame: (frame: Int16Array) => void,
  ) {
    if (!(inputRate > 0)) throw new RangeError(`sample rate ${inputRate}`)
    this.step = inputRate / targetRate
  }

  /** Takes one render quantum: one Float32Array per channel, same length. */
  push(channels: readonly Float32Array[]): void {
    const n = channels.length
    const len = channels[0]?.length ?? 0
    for (let i = 0; i < len; i++) {
      let x = 0
      for (let c = 0; c < n; c++) x += channels[c]![i]!
      x /= n
      let left = 1
      while (left > 0) {
        const take = Math.min(left, this.step - this.weight)
        this.sum += x * take
        this.weight += take
        left -= take
        if (this.weight >= this.step - 1e-9) {
          this.emit(this.sum / this.step)
          this.sum = 0
          this.weight = 0
        }
      }
    }
  }

  private emit(v: number) {
    const s = Math.max(-1, Math.min(1, v))
    this.frame[this.filled++] = Math.round(s < 0 ? s * 32768 : s * 32767)
    if (this.filled === frameSamples) {
      this.onFrame(this.frame)
      this.frame = new Int16Array(frameSamples)
      this.filled = 0
    }
  }
}

/** RMS level of s16 samples in dBFS; -Infinity for digital silence. */
export function levelDbfs(samples: Int16Array): number {
  if (samples.length === 0) return -Infinity
  let sum = 0
  for (const s of samples) sum += s * s
  const rms = Math.sqrt(sum / samples.length) / 32768
  return rms > 0 ? 20 * Math.log10(rms) : -Infinity
}
