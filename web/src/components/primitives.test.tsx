// SPDX-License-Identifier: Apache-2.0
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '../i18n'
import { EmptyState } from './EmptyState'
import { KbdHint } from './KbdHint'
import { LanguagePicker } from './LanguagePicker'
import { Panel } from './Panel'
import { QrCode } from './QrCode'
import { Stat } from './Stat'
import { StatusChip } from './StatusChip'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(() => vi.restoreAllMocks())

describe('StatusChip', () => {
  it.each([
    ['live', 'Live'],
    ['ok', 'OK'],
    ['warn', 'Attention'],
    ['error', 'Error'],
    ['idle', 'Idle'],
    ['starting', 'Starting'],
  ] as const)('%s always carries a text label (%s)', (status, label) => {
    const { container } = render(<StatusChip status={status} />)
    const chip = container.firstElementChild as HTMLElement
    expect(chip).toHaveTextContent(label)
    expect(chip.dataset.status).toBe(status)
    // The dot is decoration; the label carries the meaning.
    expect(chip.querySelector('[aria-hidden="true"]')).not.toBeNull()
  })

  it('takes a specific label and can drop the dot', () => {
    const { container } = render(<StatusChip status="ok" label="Connected" noDot />)
    expect(container).toHaveTextContent('Connected')
    expect(container.querySelector('[aria-hidden="true"]')).toBeNull()
  })

  it('translates the default label', async () => {
    await i18n.changeLanguage('es')
    render(<StatusChip status="live" />)
    expect(screen.getByText('En vivo')).toBeInTheDocument()
  })
})

describe('Panel', () => {
  it('is a region named by its title, with actions in the header', () => {
    render(
      <Panel title="Main stage" titleAs="h3" actions={<button>Pause</button>}>
        body
      </Panel>,
    )
    const region = screen.getByRole('region', { name: 'Main stage' })
    expect(within(region).getByRole('heading', { level: 3, name: 'Main stage' })).toBeVisible()
    expect(within(region).getByRole('button', { name: 'Pause' })).toBeVisible()
  })

  it('is a plain container without a title', () => {
    render(<Panel>body</Panel>)
    expect(screen.queryByRole('region')).toBeNull()
    expect(screen.getByText('body')).toBeInTheDocument()
  })
})

describe('Stat', () => {
  it('pairs the label with its value', () => {
    render(<Stat label="Latency p95" value="ES 2.4 s" detail="EN 1.3 s" />)
    expect(screen.getByRole('term')).toHaveTextContent('Latency p95')
    expect(screen.getByRole('definition')).toHaveTextContent('ES 2.4 sEN 1.3 s')
  })
})

describe('KbdHint', () => {
  it('shows Mod as Ctrl off Apple platforms', () => {
    vi.spyOn(navigator, 'platform', 'get').mockReturnValue('Win32')
    const { container } = render(<KbdHint keys={['Mod', 'K']} />)
    const keys = container.querySelectorAll('kbd kbd')
    expect([...keys].map((k) => k.textContent)).toEqual(['Ctrl', 'K'])
  })

  it('shows Mod as ⌘ on a Mac', () => {
    vi.spyOn(navigator, 'platform', 'get').mockReturnValue('MacIntel')
    const { container } = render(<KbdHint keys={['Mod', 'K']} />)
    expect(container.firstElementChild).toHaveTextContent('⌘K')
  })
})

describe('QrCode', () => {
  it('renders an SVG image named after its value, with a 4-module quiet zone', () => {
    const value = 'http://192.168.1.20:8080/s/main-stage'
    render(<QrCode value={value} />)
    const img = screen.getByRole('img', { name: `QR code for ${value}` })
    const size = Number(img.getAttribute('viewBox')?.split(' ')[2])
    // Version v is 17 + 4v modules wide, plus 4 quiet modules per side.
    expect((size - 8 - 17) % 4).toBe(0)
    const d = img.querySelector('path')?.getAttribute('d') ?? ''
    expect(d).toMatch(/^M4 4h7v1h-7z/) // top-left finder pattern starts after the quiet zone
  })

  it('takes an explicit name', () => {
    render(<QrCode value="x" label="Scan to follow the captions" />)
    expect(screen.getByRole('img', { name: 'Scan to follow the captions' })).toBeInTheDocument()
  })
})

describe('LanguagePicker', () => {
  it('names each language in itself, with a lang attribute', () => {
    render(<LanguagePicker value="es" onChange={() => {}} languages={['es', 'en', 'pt']} />)
    const select = screen.getByRole('combobox', { name: 'Caption language' })
    const options = within(select).getAllByRole('option')
    expect(options.map((o) => o.textContent)).toEqual(['Español', 'English', 'Português'])
    expect(options.map((o) => o.getAttribute('lang'))).toEqual(['es', 'en', 'pt'])
  })

  it('keeps native names when the UI is in another language', async () => {
    await i18n.changeLanguage('es')
    render(<LanguagePicker value="source" onChange={() => {}} languages={['en']} includeSource />)
    const select = screen.getByRole('combobox', { name: 'Idioma de los subtítulos' })
    expect(
      within(select)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Original (como se habla)', 'English'])
  })

  it('reports the chosen language', () => {
    const onChange = vi.fn()
    render(<LanguagePicker value="es" onChange={onChange} languages={['es', 'en']} />)
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'en' } })
    expect(onChange).toHaveBeenCalledWith('en')
  })

  it('marks an error on the field', () => {
    render(
      <LanguagePicker
        value="es"
        onChange={() => {}}
        languages={['es']}
        error
        helperText="Pick a track"
      />,
    )
    expect(screen.getByRole('combobox')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByRole('combobox')).toHaveAccessibleDescription('Pick a track')
  })
})

describe('EmptyState', () => {
  it('says what is empty, why, and offers one action', () => {
    render(
      <EmptyState
        title="No sessions yet"
        description="Create one to get the viewer URL."
        action={<button>New session</button>}
      />,
    )
    expect(screen.getByRole('heading', { level: 2, name: 'No sessions yet' })).toBeVisible()
    expect(screen.getByText('Create one to get the viewer URL.')).toBeVisible()
    expect(screen.getAllByRole('button')).toHaveLength(1)
  })
})
