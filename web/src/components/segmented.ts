// SPDX-License-Identifier: Apache-2.0
import type { SxProps, Theme } from '@mui/material/styles'

/**
 * The segmented control from design/preview.html (`.seg`): one control
 * border around the group, rule-2 hairlines between segments and the pressed
 * segment on accent-soft. Apply it to a ToggleButtonGroup.
 */
export const segmentedSx: SxProps<Theme> = {
  border: 'var(--rule-hair) solid var(--color-control)',
  borderRadius: 'var(--radius-input)',
  overflow: 'hidden',
  maxWidth: '100%',
  '& .MuiToggleButton-root': {
    border: 0,
    borderRadius: 0,
    margin: 0,
    minHeight: '2rem',
    paddingBlock: 0,
    paddingInline: 'var(--space-sm)',
    fontSize: 'var(--text-sm)',
    fontWeight: 500,
    lineHeight: 1,
    whiteSpace: 'nowrap',
    color: 'var(--color-ink-2)',
    transition:
      'background-color var(--dur-micro) var(--ease-out), color var(--dur-micro) var(--ease-out), transform var(--dur-micro) var(--ease-out)',
    '@media (pointer: coarse)': { minHeight: 'var(--control-height)' },
    '@media (hover: hover)': {
      '&:hover': { backgroundColor: 'var(--color-paper-3)' },
    },
    '&:active': { transform: 'translateY(1px)' },
    '&.Mui-selected, &.Mui-selected:hover': {
      backgroundColor: 'var(--color-accent-soft)',
      color: 'var(--color-accent-text)',
      fontWeight: 'var(--weight-strong)',
    },
    // The group clips its corners, so the ring is drawn inside the segment.
    '&.Mui-focusVisible': { outlineOffset: 'calc(-1 * var(--rule-fine))' },
    '&.Mui-disabled': { opacity: 0.5, color: 'var(--color-ink-2)' },
  },
  '& .MuiToggleButton-root + .MuiToggleButton-root': {
    borderInlineStart: 'var(--rule-hair) solid var(--color-rule-2)',
  },
}
