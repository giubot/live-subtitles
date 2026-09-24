// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest'
import { Downsampler, frameSamples, levelDbfs, targetRate } from './pcm'

/** Feeds `seconds` of f(t) at `rate` in 128-sample render quanta. */
function run(rate: number, seconds: number, ...channels: ((t: number) => number)[]) {
  const frames: Int16Array[] = []
  const ds = new Downsampler(rate, (f) => frames.push(f))
  const total = Math.round(rate * seconds)
  for (let off = 0; off < total; off += 128) {
    const n = Math.min(128, total - off)
    ds.push(channels.map((f) => Float32Array.from({ length: n }, (_, i) => f((off + i) / rate))))
  }
  return frames
}

const rms = (frames: Int16Array[]) => {
  const all = frames.flatMap((f) => [...f])
  return Math.sqrt(all.reduce((sum, s) => sum + (s / 32768) ** 2, 0) / all.length)
}

const sine =
  (hz: number, amp = 0.5) =>
  (t: number) =>
    amp * Math.sin(2 * Math.PI * hz * t)

describe('Downsampler', () => {
  it.each([48000, 44100, 16000, 8000])('turns 1 s at %i Hz into 50 frames of 20 ms', (rate) => {
    const frames = run(rate, 1, sine(440))
    expect(frames.length).toBeGreaterThanOrEqual(49)
    expect(frames.length).toBeLessThanOrEqual(50)
    for (const f of frames) expect(f).toHaveLength(frameSamples)
  })

  it('downmixes channels to mono', () => {
    const frames = run(
      48000,
      0.1,
      () => 0.5,
      () => -0.5,
    )
    expect(rms(frames)).toBe(0)
    const dc = run(
      48000,
      0.1,
      () => 0.5,
      () => 0.5,
    )
    expect(new Set(dc.flatMap((f) => [...f]))).toEqual(new Set([16384]))
  })

  it('clamps out-of-range input', () => {
    const [hi] = run(48000, 0.02, () => 2)
    const [lo] = run(48000, 0.02, () => -2)
    expect(hi?.[0]).toBe(32767)
    expect(lo?.[0]).toBe(-32768)
  })

  it('keeps speech frequencies and damps what would alias', () => {
    const inputRms = 0.5 / Math.SQRT2
    expect(rms(run(48000, 0.5, sine(1000)))).toBeGreaterThan(inputRms * 0.95)
    // 12 kHz is above the 8 kHz Nyquist limit of 16 kHz audio.
    expect(rms(run(48000, 0.5, sine(12000)))).toBeLessThan(inputRms * 0.4)
  })

  it('outputs 16 kHz', () => {
    expect(targetRate).toBe(16000)
  })
})

describe('levelDbfs', () => {
  it.each([
    ['silence', new Int16Array(320), -Infinity],
    ['empty', new Int16Array(0), -Infinity],
    ['half scale', new Int16Array(320).fill(16384), -6.02],
    ['full scale', new Int16Array(320).fill(-32768), 0],
  ])('%s', (_, samples, want) => {
    const got = levelDbfs(samples)
    if (Number.isFinite(want)) expect(got).toBeCloseTo(want, 1)
    else expect(got).toBe(want)
  })
})
