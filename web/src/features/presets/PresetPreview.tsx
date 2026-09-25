// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import { useTranslation } from 'react-i18next'
import { OverlayCaptions } from '../overlay/OverlayCaptions'
import { cqw, type OverlayLook } from '../overlay/overlayStyle'

/**
 * The overlay as OBS would show it: the real caption box over a neutral
 * 16:9 program frame (`.program` in design/preview.html), scaled from
 * 1920 × 1080 to the frame's width. Never follows the UI theme's text
 * colours; only the frame behind it does.
 */
export function PresetPreview({ look }: { look: OverlayLook }) {
  const { t, i18n } = useTranslation('presets')
  const lang = (i18n.resolvedLanguage ?? i18n.language).split('-')[0]
  const lines = [
    { id: 'a', text: t('preview.line1'), lang },
    { id: 'b', text: t('preview.line2'), lang },
    ...(look.showInterim ? [{ id: 'c', text: t('preview.interim'), lang }] : []),
  ]
  return (
    <Box
      role="img"
      aria-label={t('preview.label')}
      sx={{
        position: 'relative',
        aspectRatio: '16 / 9',
        overflow: 'hidden',
        borderRadius: 'var(--radius-input)',
        backgroundColor: 'var(--color-graphite)',
        containerType: 'inline-size',
      }}
    >
      <Box
        aria-hidden
        sx={{
          position: 'absolute',
          insetBlockStart: '3cqw',
          insetInlineStart: '3cqw',
          fontFamily: 'var(--font-mono)',
          fontSize: '1.4cqw',
          letterSpacing: 'var(--tracking-label)',
          textTransform: 'uppercase',
          color: 'var(--color-graphite-ink)',
          opacity: 0.7,
        }}
      >
        {t('preview.frame')}
      </Box>
      <Box
        aria-hidden
        sx={{
          position: 'absolute',
          inset: '18% 30% 34%',
          display: 'grid',
          placeItems: 'center',
          padding: '1cqw',
          border: 'var(--rule-hair) dashed var(--color-graphite-rule)',
          borderRadius: 'var(--radius-input)',
          fontFamily: 'var(--font-mono)',
          fontSize: '1.6cqw',
          textAlign: 'center',
          color: 'var(--color-graphite-ink)',
          opacity: 0.6,
        }}
      >
        {t('preview.slate')}
      </Box>
      <OverlayCaptions
        look={look}
        lines={lines}
        visible
        unit={cqw}
        placement="absolute"
        live={false}
      />
    </Box>
  )
}
