// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import type { SxProps, Theme } from '@mui/material/styles'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { encode } from 'uqr'

/** Quiet zone in modules; the QR spec asks for 4. */
const quietZone = 4

/** One SVG path for all dark modules, merging horizontal runs to keep it small. */
function modulesPath(data: boolean[][]): string {
  let d = ''
  data.forEach((row, y) => {
    let x = 0
    while (x < row.length) {
      if (!row[x]) {
        x++
        continue
      }
      const start = x
      while (row[x]) x++
      d += `M${start} ${y}h${x - start}v1h${start - x}z`
    }
  })
  return d
}

export interface QrCodeProps {
  /** The text to encode, usually a viewer URL. */
  value: string
  /** Rendered edge length (any CSS length). */
  size?: string
  /** Accessible name. Defaults to "QR code for <value>". */
  label?: string
  /** Error correction; M survives a smudged projector screen without growing much. */
  ecc?: 'L' | 'M' | 'Q' | 'H'
  sx?: SxProps<Theme>
}

/**
 * QR code as inline SVG (uqr). Always dark modules on a light field with a
 * 4-module quiet zone, whatever the UI theme: inverted codes scan badly.
 */
export function QrCode({ value, size = '8rem', label, ecc = 'M', sx }: QrCodeProps) {
  const { t } = useTranslation()
  const { path, width } = useMemo(() => {
    const { data, size: n } = encode(value, { ecc, border: quietZone })
    return { path: modulesPath(data), width: n }
  }, [value, ecc])

  return (
    <Box
      component="svg"
      role="img"
      aria-label={label ?? t('qr.label', { value })}
      viewBox={`0 0 ${width} ${width}`}
      shapeRendering="crispEdges"
      sx={[
        {
          display: 'block',
          inlineSize: size,
          blockSize: size,
          maxInlineSize: '100%',
          borderRadius: 'var(--radius-chip)',
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      <rect width={width} height={width} style={{ fill: 'var(--stage-fg-white)' }} />
      <path d={path} style={{ fill: 'var(--stage-bg-dark)' }} />
    </Box>
  )
}
