// SPDX-License-Identifier: Apache-2.0
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import i18n from '../i18n'
import { LevelMeter } from './LevelMeter'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

function segments() {
  return Array.from(screen.getByRole('meter').children) as HTMLElement[]
}
const lit = () => segments().filter((s) => s.dataset.on)

describe('LevelMeter', () => {
  it('exposes the level as a meter', () => {
    render(<LevelMeter db={-14.4} />)
    const meter = screen.getByRole('meter', { name: 'Input level' })
    expect(meter).toHaveAttribute('aria-valuemin', '-60')
    expect(meter).toHaveAttribute('aria-valuemax', '0')
    expect(meter).toHaveAttribute('aria-valuenow', '-14')
    expect(meter).toHaveAttribute('aria-valuetext', '-14 dBFS')
  })

  it('has 24 segments: ok, then warn-fill near the top, then danger at clipping', () => {
    render(<LevelMeter db={0} />)
    const zones = segments().map((s) => s.dataset.zone)
    expect(zones).toHaveLength(24)
    // Defaults: 2.5 dB per segment, warn from -12 dBFS, clip from -3 dBFS.
    expect(zones.slice(0, 20).every((z) => z === 'ok')).toBe(true)
    expect(zones.slice(20, 23)).toEqual(['warn', 'warn', 'warn'])
    expect(zones[23]).toBe('clip')
    expect(lit()).toHaveLength(24)
  })

  it.each([
    { db: -60, on: 0 },
    { db: -59, on: 1 },
    { db: -30, on: 12 },
    { db: -14, on: 19 },
    { db: -10, on: 20 },
    { db: -9.9, on: 21 },
    { db: -2, on: 24 },
    { db: 6, on: 24 },
  ])('lights $on segments at $db dBFS', ({ db, on }) => {
    render(<LevelMeter db={db} />)
    expect(lit()).toHaveLength(on)
  })

  it('reads silence and clipping in words and clamps out-of-range values', () => {
    const { rerender } = render(<LevelMeter db={-Infinity} />)
    const meter = screen.getByRole('meter')
    expect(meter).toHaveAttribute('aria-valuenow', '-60')
    expect(meter).toHaveAttribute('aria-valuetext', 'Silence')
    expect(lit()).toHaveLength(0)

    rerender(<LevelMeter db={3} />)
    expect(meter).toHaveAttribute('aria-valuenow', '0')
    expect(meter).toHaveAttribute('aria-valuetext', '0 dBFS, clipping')
  })

  it('respects custom thresholds and a custom name', () => {
    render(<LevelMeter db={-20} min={-48} max={0} warnAt={-24} clipAt={-6} label="Room B input" />)
    const zones = segments().map((s) => s.dataset.zone)
    // 2 dB per segment: lower edges -48, -46, … -2.
    expect(zones.indexOf('warn')).toBe(12)
    expect(zones.indexOf('clip')).toBe(21)
    expect(screen.getByRole('meter', { name: 'Room B input' })).toHaveAttribute(
      'aria-valuemin',
      '-48',
    )
  })

  it('translates the reading', async () => {
    await i18n.changeLanguage('es')
    render(<LevelMeter db={-1} />)
    expect(screen.getByRole('meter', { name: 'Nivel de entrada' })).toHaveAttribute(
      'aria-valuetext',
      '-1 dBFS, saturando',
    )
  })
})
