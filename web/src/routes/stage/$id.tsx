// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

// Stage screen: its own high-contrast presets, never the UI theme (UI-7).
// The real page lands in P1-15.
export const Route = createFileRoute('/stage/$id')({
  component: Stage,
})

function Stage() {
  const { t } = useTranslation('stage')
  return (
    <main
      style={{
        minHeight: '100dvh',
        display: 'grid',
        placeItems: 'center',
        background: 'var(--stage-bg-dark)',
        color: 'var(--stage-fg-white)',
        fontFamily: 'var(--font-body)',
        fontSize: 'var(--text-caption-stage)',
      }}
    >
      <p style={{ margin: 0 }}>{t('waiting')}</p>
    </main>
  )
}
