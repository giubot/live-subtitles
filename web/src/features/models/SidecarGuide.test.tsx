// SPDX-License-Identifier: Apache-2.0
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import type { Schemas } from '../../api/types'
import i18n from '../../i18n'
import { SidecarGuide, type SidecarGuideProps } from './SidecarGuide'
import { parseAddr, whisperDefault, whisperFile } from './sidecarSteps'

type HardwareReport = Schemas['HardwareReport']

const report = (over: Partial<HardwareReport> = {}): HardwareReport => ({
  os: 'darwin',
  arch: 'arm64',
  cpu: { model: 'Apple M2 Pro', cores: 12 },
  memoryBytes: 16 * 1024 ** 3,
  gpus: [],
  runtimes: {
    whisper: { reachable: false, url: 'http://127.0.0.1:8178' },
    ollama: { reachable: false, url: 'http://127.0.0.1:11434' },
    ffmpeg: { reachable: true },
  },
  recommendation: {
    whisperModel: 'large-v3-turbo',
    gemmaModel: 'gemma3:4b',
    localRealtimeLikely: true,
  },
  modelsDir: '/Users/ana/livesubs/models',
  ...over,
})

const runtimes = (whisper: boolean, ollama: boolean, urls: { w?: string; o?: string } = {}) => ({
  whisper: { reachable: whisper, url: urls.w ?? 'http://127.0.0.1:8178' },
  ollama: { reachable: ollama, url: urls.o ?? 'http://127.0.0.1:11434' },
  ffmpeg: { reachable: true },
})

async function renderGuide(props: SidecarGuideProps, open = true) {
  const rootRoute = createRootRoute({ component: () => <SidecarGuide {...props} /> })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(<RouterProvider router={router} />)
  const toggle = await screen.findByRole('button', { name: /How to install/ })
  if (open) await userEvent.click(toggle)
  return toggle
}

const commands = () => Array.from(document.querySelectorAll('code')).map((c) => c.textContent ?? '')

const pickOs = (name: string) => userEvent.click(screen.getByRole('button', { name }))

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('SidecarGuide', () => {
  it('is collapsed behind a disclosure with aria-expanded', async () => {
    const toggle = await renderGuide({ report: report() }, false)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveAccessibleName('How to install and start whisper-server · Ollama')
    expect(screen.queryByRole('region')).not.toBeInTheDocument()

    await userEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    const region = screen.getByRole('region', { name: 'Install whisper-server and Ollama' })
    expect(toggle).toHaveAttribute('aria-controls', region.id)

    await userEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })

  it.each([
    ['darwin', 'macOS'],
    ['linux', 'Linux'],
    ['windows', 'Windows'],
    ['freebsd', 'Docker'],
  ])('preselects the tab for os %s', async (os, tab) => {
    await renderGuide({ report: report({ os }) })
    const group = screen.getByRole('group', { name: 'Operating system' })
    expect(group).toBeInTheDocument()
    expect(screen.getByRole('button', { name: tab })).toHaveAttribute('aria-pressed', 'true')
    for (const other of ['macOS', 'Linux', 'Windows', 'Docker'].filter((n) => n !== tab))
      expect(screen.getByRole('button', { name: other })).toHaveAttribute('aria-pressed', 'false')
  })

  it.each<{
    name: string
    tab: string
    over: Partial<HardwareReport>
    model?: string
    want: string[]
  }>([
    {
      name: 'macOS, native with the parsed address',
      tab: 'macOS',
      over: {
        runtimes: runtimes(false, false, { w: 'http://10.0.0.5:9000', o: 'http://10.0.0.5:11500' }),
        modelsDir: '/srv/livesubs/models/',
      },
      model: 'small',
      want: [
        'brew install whisper.cpp ollama',
        'OLLAMA_HOST=10.0.0.5:11500 ollama serve',
        'whisper-server --host 10.0.0.5 --port 9000 --model "/srv/livesubs/models/ggml-small.bin"',
      ],
    },
    {
      name: 'Linux, Ollama script and whisper-server in Docker',
      tab: 'Linux',
      over: { runtimes: runtimes(false, false, { w: 'http://127.0.0.1:9000' }) },
      model: 'medium',
      want: [
        'curl -fsSL https://ollama.com/install.sh | sh',
        'docker run -d --name whisper-server -p 9000:8178 -v "/Users/ana/livesubs/models:/models:ro" ghcr.io/ggml-org/whisper.cpp:main "./build/bin/whisper-server --host 0.0.0.0 --port 8178 --model /models/ggml-medium.bin"',
      ],
    },
    {
      name: 'Windows, with backslashes',
      tab: 'Windows',
      over: { os: 'windows', modelsDir: 'C:\\livesubs\\models' },
      want: [
        'winget install Ollama.Ollama',
        '.\\whisper-server.exe --host 127.0.0.1 --port 8178 --model "C:\\livesubs\\models\\ggml-large-v3-turbo.bin"',
      ],
    },
    {
      name: 'Docker, both containers',
      tab: 'Docker',
      over: { runtimes: runtimes(false, false, { o: 'http://127.0.0.1:11999' }) },
      model: 'small',
      want: [
        'docker run -d --name ollama -p 11999:11434 -v ollama:/root/.ollama ollama/ollama',
        'docker run -d --name whisper-server -p 8178:8178 -v "/Users/ana/livesubs/models:/models:ro" ghcr.io/ggml-org/whisper.cpp:main "./build/bin/whisper-server --host 0.0.0.0 --port 8178 --model /models/ggml-small.bin"',
      ],
    },
  ])('builds the commands: $name', async ({ tab, over, model, want }) => {
    await renderGuide({ report: report(over), whisperModel: model })
    await pickOs(tab)
    expect(commands()).toEqual(want)
  })

  it('names the models new sessions use in the download step', async () => {
    await renderGuide({ report: report(), whisperModel: 'medium', gemmaModel: 'gemma3:1b' })
    expect(
      screen.getByText(/Download the speech model medium and the translation model gemma3:1b/),
    ).toBeInTheDocument()
  })

  it.each([
    ['macOS', '"<models folder>/ggml-large-v3-turbo.bin"'],
    ['Windows', '"<models folder>\\ggml-large-v3-turbo.bin"'],
    ['Docker', '-v "<models folder>:/models:ro"'],
  ])('shows a placeholder on %s when the server has no modelsDir', async (tab, want) => {
    await renderGuide({ report: report({ modelsDir: undefined }) })
    await pickOs(tab)
    expect(commands().some((c) => c.includes(want))).toBe(true)
    expect(screen.getByText(/Replace <models folder> with .*--models-dir/)).toBeInTheDocument()
  })

  it('has no placeholder hint when the server says where its models are', async () => {
    await renderGuide({ report: report() })
    expect(screen.queryByText(/Replace <models folder>/)).not.toBeInTheDocument()
  })

  it.each<{
    name: string
    whisper: boolean
    ollama: boolean
    tab: string
    want: string[]
    chip?: string
  }>([
    {
      name: 'only whisper-server missing, macOS',
      whisper: false,
      ollama: true,
      tab: 'macOS',
      want: [
        'brew install whisper.cpp',
        'whisper-server --host 127.0.0.1 --port 8178 --model "/Users/ana/livesubs/models/ggml-large-v3-turbo.bin"',
      ],
      chip: 'Ollama already running',
    },
    {
      name: 'only Ollama missing, macOS',
      whisper: true,
      ollama: false,
      tab: 'macOS',
      want: ['brew install ollama', 'ollama serve'],
      chip: 'whisper-server already running',
    },
    {
      name: 'only Ollama missing, Docker',
      whisper: true,
      ollama: false,
      tab: 'Docker',
      want: ['docker run -d --name ollama -p 11434:11434 -v ollama:/root/.ollama ollama/ollama'],
      chip: 'whisper-server already running',
    },
    {
      name: 'only whisper-server missing, Windows',
      whisper: false,
      ollama: true,
      tab: 'Windows',
      want: [
        '.\\whisper-server.exe --host 127.0.0.1 --port 8178 --model "/Users/ana/livesubs/models\\ggml-large-v3-turbo.bin"',
      ],
      chip: 'Ollama already running',
    },
  ])(
    'gives start steps only to missing sidecars: $name',
    async ({ whisper, ollama, tab, want, chip }) => {
      await renderGuide({ report: report({ runtimes: runtimes(whisper, ollama) }) })
      await pickOs(tab)
      expect(commands()).toEqual(want)
      if (chip) expect(screen.getByText(chip)).toBeInTheDocument()
      const sidecars = Array.from(document.querySelectorAll('li[data-sidecar]')).map((li) =>
        li.getAttribute('data-sidecar'),
      )
      expect(sidecars.every((s) => (s === 'whisper' ? !whisper : !ollama))).toBe(true)
    },
  )

  it('shows every step for reference when both run', async () => {
    const toggle = await renderGuide({ report: report({ runtimes: runtimes(true, true) }) })
    expect(toggle).toHaveAccessibleName('How to install whisper-server and Ollama')
    expect(screen.getByText(/Both are running/)).toBeInTheDocument()
    expect(commands()).toEqual([
      'brew install whisper.cpp ollama',
      'ollama serve',
      'whisper-server --host 127.0.0.1 --port 8178 --model "/Users/ana/livesubs/models/ggml-large-v3-turbo.bin"',
    ])
  })

  it('links to Settings and notes the restart after a model change', async () => {
    await renderGuide({ report: report() })
    expect(screen.getByRole('link', { name: 'Open Settings' })).toHaveAttribute(
      'href',
      '/admin/settings',
    )
    expect(screen.getByText(/loads one model when it starts/)).toBeInTheDocument()
  })

  it('tells Mac users that Docker is slower there', async () => {
    await renderGuide({ report: report() })
    await pickOs('Docker')
    expect(screen.getByText(/can’t use the Apple GPU/)).toBeInTheDocument()
    expect(screen.getByText(/main-cuda/)).toBeInTheDocument()
  })
})

describe('sidecarSteps', () => {
  it.each([
    ['http://127.0.0.1:8178', { host: '127.0.0.1', port: '8178' }],
    ['http://whisper.lan', { host: 'whisper.lan', port: '80' }],
    ['https://whisper.lan', { host: 'whisper.lan', port: '443' }],
    ['http://[::1]:9000', { host: '::1', port: '9000' }],
    ['not a url', whisperDefault],
    [undefined, whisperDefault],
  ])('parses %s', (url, want) => {
    expect(parseAddr(url, whisperDefault)).toEqual(want)
  })

  it.each([
    ['macos', '/srv/models', '/srv/models/ggml-small.bin'],
    ['linux', '/srv/models//', '/srv/models/ggml-small.bin'],
    ['windows', 'D:\\models\\', 'D:\\models\\ggml-small.bin'],
  ] as const)('joins the whisper file on %s', (os, dir, want) => {
    expect(whisperFile(os, dir, 'small')).toBe(want)
  })
})
