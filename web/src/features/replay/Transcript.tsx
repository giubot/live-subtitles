// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import type { Caption } from '../../api/types'
import { timecode } from '../viewer/format'

export interface TranscriptProps {
  cues: readonly Caption[]
  /** Index of the cue playing now, -1 for none. */
  current: number
  /** `lang` of the cue text (the track, or each cue's source language). */
  langOf: (c: Caption) => string
  onSeek: (seconds: number) => void
}

/**
 * The recording's captions as a list of lines: the one playing is marked
 * (accent tick, aria-current) and kept in view; clicking a line plays the
 * audio from there.
 */
export function Transcript({ cues, current, langOf, onSeek }: TranscriptProps) {
  const { t } = useTranslation('replay')
  const listRef = useRef<HTMLOListElement>(null)

  useEffect(() => {
    if (current < 0) return
    const el = listRef.current?.querySelector<HTMLElement>(`[data-cue="${current}"]`)
    el?.scrollIntoView?.({ block: 'nearest' })
  }, [current])

  return (
    <Box
      component="ol"
      ref={listRef}
      aria-label={t('transcript')}
      sx={{ listStyle: 'none', margin: 0, padding: 0, display: 'grid', gap: 'var(--space-3xs)' }}
    >
      {cues.map((c, i) => {
        const active = i === current
        return (
          <li key={c.segmentId} data-cue={i}>
            <Box
              component="button"
              type="button"
              onClick={() => onSeek(c.start)}
              aria-current={active || undefined}
              sx={{
                all: 'unset',
                boxSizing: 'border-box',
                cursor: 'pointer',
                inlineSize: '100%',
                display: 'grid',
                gap: 'var(--space-3xs)',
                paddingBlock: 'var(--space-xs)',
                paddingInline: 'var(--space-sm)',
                borderRadius: 'var(--radius-input)',
                borderInlineStart: 'var(--space-2xs) solid transparent',
                backgroundColor: active ? 'var(--color-accent-soft)' : 'transparent',
                borderInlineStartColor: active ? 'var(--color-accent)' : 'transparent',
                transition: 'background-color var(--dur-micro) var(--ease-out)',
                '@media (hover: hover)': {
                  '&:hover': {
                    backgroundColor: active ? 'var(--color-accent-soft)' : 'var(--color-paper-3)',
                  },
                },
                '&:focus-visible': {
                  outline: 'var(--rule-fine) solid var(--color-focus)',
                  outlineOffset: 'var(--rule-fine)',
                },
              }}
            >
              <Box
                component="time"
                sx={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: 'var(--text-xs)',
                  lineHeight: 1,
                  color: active ? 'var(--color-accent-text)' : 'var(--color-muted)',
                  fontVariantNumeric: 'tabular-nums',
                }}
              >
                {timecode(c.start)}
              </Box>
              <Box
                component="span"
                lang={langOf(c)}
                sx={{
                  fontFamily: 'var(--font-body)',
                  fontSize: 'var(--text-lg)',
                  lineHeight: 'var(--leading-body)',
                  maxInlineSize: 'var(--measure)',
                  color: active ? 'var(--color-ink)' : 'var(--color-ink-2)',
                }}
              >
                {c.text}
              </Box>
            </Box>
          </li>
        )
      })}
    </Box>
  )
}
