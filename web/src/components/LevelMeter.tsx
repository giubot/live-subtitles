// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import type { SxProps, Theme } from '@mui/material/styles'
import { useTranslation } from 'react-i18next'

const segments = 24

type Zone = 'ok' | 'warn' | 'clip'

const zoneColour: Record<Zone, string> = {
  ok: 'var(--color-ok)',
  warn: 'var(--color-warn-fill)',
  clip: 'var(--color-danger)',
}

export interface LevelMeterProps {
  /** Current level in dBFS (0 is full scale). -Infinity or NaN reads as silence. */
  db: number
  /** Bottom of the scale in dBFS. */
  min?: number
  /** Top of the scale in dBFS. */
  max?: number
  /** Segments starting at or above this level light in warn-fill ("hot"). */
  warnAt?: number
  /** Segments starting at or above this level light in danger (clipping). */
  clipAt?: number
  /** `lg` is the capture page's big meter. */
  size?: 'sm' | 'lg'
  /** Accessible name. Defaults to "Input level". */
  label?: string
  sx?: SxProps<Theme>
}

/**
 * 24-segment audio level meter: ok → warn-fill near the top → danger at
 * clipping (docs/design.md § Components). Segment i covers
 * [min + i·step, min + (i+1)·step) and lights once the level passes its lower
 * edge; its colour comes from that lower edge.
 */
export function LevelMeter({
  db,
  min = -60,
  max = 0,
  warnAt = -12,
  clipAt = -3,
  size = 'sm',
  label,
  sx,
}: LevelMeterProps) {
  const { t, i18n } = useTranslation()
  const level = Number.isFinite(db) ? Math.min(max, Math.max(min, db)) : min
  const step = (max - min) / segments
  const rounded = Math.round(level)
  const valueText =
    level <= min
      ? t('meter.silent')
      : t(level >= clipAt ? 'meter.clipping' : 'meter.value', {
          value: new Intl.NumberFormat(i18n.resolvedLanguage).format(rounded),
        })

  return (
    <Box
      role="meter"
      aria-label={label ?? t('meter.label')}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={rounded}
      aria-valuetext={valueText}
      sx={[
        {
          display: 'grid',
          gridTemplateColumns: `repeat(${segments}, minmax(0, 1fr))`,
          gap: size === 'lg' ? 'var(--space-2xs)' : 'var(--space-3xs)',
          blockSize: size === 'lg' ? '1.5rem' : '0.625rem',
          minInlineSize: 0,
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      {Array.from({ length: segments }, (_, i) => {
        const lower = min + i * step
        const zone: Zone = lower >= clipAt ? 'clip' : lower >= warnAt ? 'warn' : 'ok'
        const on = level > lower
        return (
          <Box
            key={i}
            component="span"
            data-zone={zone}
            data-on={on || undefined}
            sx={{
              borderRadius: 'var(--rule-hair)',
              backgroundColor: on ? zoneColour[zone] : 'var(--color-paper-3)',
            }}
          />
        )
      })}
    </Box>
  )
}
