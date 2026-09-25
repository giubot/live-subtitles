// SPDX-License-Identifier: Apache-2.0
import CheckOutlined from '@mui/icons-material/CheckOutlined'
import ContentCopyOutlined from '@mui/icons-material/ContentCopyOutlined'
import ErrorOutlineOutlined from '@mui/icons-material/ErrorOutlineOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import type { SxProps, Theme } from '@mui/material/styles'
import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { copyText } from './clipboard'

/** How long the button says "Copied" before it resets. */
const resetMs = 2000

export type CopyState = 'idle' | 'copied' | 'failed'

export interface CopyFieldProps {
  /** The URL or token to show and copy. */
  value: string
  /** Mono uppercase label above the value ("Overlay URL · 1920 × 1080"). */
  label?: ReactNode
  /** Visible button text. Defaults to "Copy". */
  copyLabel?: string
  /** Disables copying; say why in `disabledReason`. */
  disabled?: boolean
  disabledReason?: ReactNode
  onCopied?: () => void
  /** Pins the copy result on screen; only for /dev/design and tests. */
  forceState?: CopyState
  /**
   * `card` (default) is the URL/token card. `command` is a compact block for
   * a shell command: tighter padding, a small button, spaces kept as typed.
   */
  variant?: 'card' | 'command'
  sx?: SxProps<Theme>
}

/**
 * Graphite URL/token card with a copy button (`.urlcard` in
 * design/preview.html). Copy works outside secure contexts too, and the
 * result is announced politely to screen readers.
 */
export function CopyField({
  value,
  label,
  copyLabel,
  disabled,
  disabledReason,
  onCopied,
  forceState,
  variant = 'card',
  sx,
}: CopyFieldProps) {
  const command = variant === 'command'
  const { t } = useTranslation()
  const [copyState, setState] = useState<CopyState>('idle')
  const state = forceState ?? copyState
  const timer = useRef<number | undefined>(undefined)
  const valueRef = useRef<HTMLElement>(null)
  const valueId = useId()
  const reasonId = useId()
  useEffect(() => () => window.clearTimeout(timer.current), [])

  const copy = async () => {
    window.clearTimeout(timer.current)
    if (await copyText(value)) {
      setState('copied')
      onCopied?.()
      timer.current = window.setTimeout(() => setState('idle'), resetMs)
      return
    }
    setState('failed')
    // Leave the text selected so a manual Ctrl+C / ⌘C works.
    const node = valueRef.current
    const selection = window.getSelection()
    if (node && selection) {
      const range = document.createRange()
      range.selectNodeContents(node)
      selection.removeAllRanges()
      selection.addRange(range)
    }
  }

  const icon =
    state === 'copied' ? (
      <CheckOutlined aria-hidden />
    ) : state === 'failed' ? (
      <ErrorOutlineOutlined aria-hidden />
    ) : (
      <ContentCopyOutlined aria-hidden />
    )

  return (
    <Box
      sx={[
        {
          backgroundColor: 'var(--color-graphite)',
          color: 'var(--color-graphite-ink)',
          borderRadius: 'var(--radius-card)',
          padding: command ? 'var(--space-xs) var(--space-sm)' : 'var(--space-md)',
          display: 'grid',
          gap: command ? 'var(--space-xs)' : 'var(--space-sm)',
          minInlineSize: 0,
          position: 'relative',
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      {label != null && (
        <Box
          component="span"
          sx={{
            fontFamily: 'var(--font-mono)',
            fontSize: 'var(--text-xs)',
            fontWeight: 500,
            lineHeight: 1.3,
            letterSpacing: 'var(--tracking-label)',
            textTransform: 'uppercase',
            opacity: 0.75,
          }}
        >
          {label}
        </Box>
      )}
      <Box
        sx={{
          // The button drops under the value when the card is narrow.
          display: 'flex',
          flexWrap: 'wrap',
          gap: 'var(--space-sm)',
          alignItems: 'center',
          '& > code': { flex: '1 1 14rem', minInlineSize: 0 },
          '& > button': { flex: 'none' },
        }}
      >
        <Box
          component="code"
          id={valueId}
          ref={valueRef}
          sx={{
            fontFamily: 'var(--font-mono)',
            fontSize: 'var(--text-sm)',
            lineHeight: 1.4,
            overflowWrap: 'anywhere',
            whiteSpace: command ? 'pre-wrap' : undefined,
            '&::selection': {
              backgroundColor: 'var(--color-accent)',
              color: 'var(--color-accent-ink)',
            },
          }}
        >
          {value}
        </Box>
        <Button
          variant="outlined"
          size={command ? 'small' : undefined}
          onClick={() => void copy()}
          disabled={disabled}
          startIcon={icon}
          aria-describedby={[valueId, disabled && disabledReason ? reasonId : null]
            .filter(Boolean)
            .join(' ')}
          sx={{
            color: 'var(--color-graphite-ink)',
            borderColor: 'var(--color-graphite-rule)',
            backgroundColor: 'transparent',
            whiteSpace: 'nowrap',
            '@media (hover: hover)': {
              '&:hover': {
                color: 'var(--color-graphite-ink)',
                borderColor: 'var(--color-graphite-ink)',
                backgroundColor: 'transparent',
              },
            },
            '&.Mui-disabled': {
              color: 'var(--color-graphite-ink)',
              border: 'var(--rule-hair) solid var(--color-graphite-rule)',
            },
          }}
        >
          {state === 'copied'
            ? t('copy.done')
            : state === 'failed'
              ? t('copy.retry')
              : (copyLabel ?? t('copy.action'))}
        </Button>
      </Box>
      {disabled && disabledReason != null && (
        <Box id={reasonId} component="span" sx={{ fontSize: 'var(--text-sm)', opacity: 0.75 }}>
          {disabledReason}
        </Box>
      )}
      {state === 'failed' && (
        <Box
          sx={{
            display: 'flex',
            gap: 'var(--space-xs)',
            alignItems: 'flex-start',
            fontSize: 'var(--text-sm)',
          }}
        >
          <ErrorOutlineOutlined aria-hidden fontSize="small" />
          <span>{t('copy.failed')}</span>
        </Box>
      )}
      {/* Polite announcement: the button text change alone isn't read out. */}
      <Box
        role="status"
        sx={{
          position: 'absolute',
          inlineSize: '1px',
          blockSize: '1px',
          overflow: 'hidden',
          clipPath: 'inset(50%)',
          whiteSpace: 'nowrap',
        }}
      >
        {state === 'copied' ? t('copy.done') : state === 'failed' ? t('copy.failed') : ''}
      </Box>
    </Box>
  )
}
