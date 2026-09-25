// SPDX-License-Identifier: Apache-2.0
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Schemas } from '../../api/types'
import i18n from '../../i18n'
import { emptyAdminEvents, useAdminEventsStore } from '../../realtime/admin'
import { routeTree } from '../../routeTree.gen'
import { fakeApi, type FakeRoute } from '../../test/fakeApi'
import { FakeWebSocket, FakeWebSocketFactory } from '../../test/fakeWebSocket'
import { busy } from './useModelProgress'

type HardwareReport = Schemas['HardwareReport']
type LocalModel = Schemas['LocalModel']
type Settings = Schemas['Settings']

function renderAt(path: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
    context: { queryClient },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return router
}

const hardware = (over: Partial<HardwareReport['runtimes']> = {}): HardwareReport => ({
  os: 'darwin',
  arch: 'arm64',
  cpu: { model: 'Apple M2 Pro', cores: 12 },
  memoryBytes: 16 * 1024 ** 3,
  gpus: [{ name: 'Apple M2 Pro', backend: 'metal' }],
  runtimes: {
    whisper: { reachable: true, version: '1.7.4', url: 'http://127.0.0.1:8178' },
    ollama: { reachable: true, version: '0.6.2', url: 'http://127.0.0.1:11434' },
    ffmpeg: { reachable: true, version: '7.1' },
    ...over,
  },
  recommendation: {
    whisperModel: 'large-v3-turbo',
    gemmaModel: 'gemma3:4b',
    localRealtimeLikely: true,
  },
})

const small: LocalModel = {
  id: 'whisper-small',
  kind: 'whisper',
  name: 'small',
  sizeBytes: 488e6,
  status: 'missing',
}

const catalog = (): LocalModel[] => [
  small,
  {
    id: 'whisper-large-v3-turbo',
    kind: 'whisper',
    name: 'large-v3-turbo',
    sizeBytes: 1.6e9,
    recommended: true,
    status: 'ready',
  },
  {
    id: 'gemma3-4b',
    kind: 'gemma',
    name: 'gemma3:4b',
    sizeBytes: 3.3e9,
    recommended: true,
    status: 'ready',
  },
  { id: 'gemma3-1b', kind: 'gemma', name: 'gemma3:1b', sizeBytes: 815e6, status: 'ready' },
  { id: 'whisper-base', kind: 'whisper', name: 'base', sizeBytes: 148e6, status: 'ready' },
]

const settings: Settings = {
  defaultSourceLanguage: 'auto',
  defaultTargetLanguages: ['es', 'en'],
  defaultGlossaryId: null,
  providers: {
    gemini: { liveModel: 'gemini-live-2.5-flash', translationModel: 'gemini-2.5-flash' },
    local: {
      whisperUrl: 'http://127.0.0.1:8178',
      whisperModel: 'large-v3-turbo',
      ollamaUrl: 'http://127.0.0.1:11434',
      gemmaModel: 'gemma3:4b',
    },
    fallback: true,
  },
  translation: { contextSentences: 3 },
  captions: { maxCharsPerLine: 42, maxLines: 2 },
  network: { publicBaseUrl: 'https://subs.example.org', preferredInterface: null },
  recording: { enabledByDefault: true, bitrateKbps: 48, retentionDays: 30 },
  obs: { websocketUrl: 'ws://127.0.0.1:4455' },
  srt: { enabled: true, port: 9000, latencyMs: 200, passphraseSet: false },
}

/** The admin shell plus this page's endpoints; `routes` override any of them. */
function serve(routes: Record<string, FakeRoute> = {}) {
  return fakeApi({
    'GET /api/auth/me': () => [200, { authenticated: true }],
    'GET /api/network': () => [
      200,
      { interfaces: [], preferredIp: '192.168.1.20', httpPort: 8080 },
    ],
    'GET /api/system/hardware': () => [200, hardware()],
    'GET /api/models': () => [200, catalog()],
    'GET /api/settings': () => [200, settings],
    ...routes,
  })
}

/** Both the shell and the page follow /ws/admin; play the server on every one. */
function adminSockets() {
  return FakeWebSocket.instances.filter((ws) => ws.url.endsWith('/ws/admin') && ws.readyState < 3)
}
function acceptAll() {
  act(() => adminSockets().forEach((ws) => ws.accept()))
}
function push(model: LocalModel) {
  act(() => adminSockets().forEach((ws) => ws.receive({ type: 'modelProgress', model })))
}

async function modelsRegion() {
  return screen.findByRole('region', { name: 'Local models' })
}
/** The catalog row that shows the model called `name`. */
async function row(name: string) {
  const region = await modelsRegion()
  const title = await within(region).findByText(name, { exact: true })
  const li = title.closest('li')
  if (!li) throw new Error(`no row for ${name}`)
  return within(li)
}

const getsOf = (calls: { method: string; path: string }[], path: string) =>
  calls.filter((c) => c.method === 'GET' && c.path === path).length

describe('models page', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    FakeWebSocket.reset()
    vi.stubGlobal('WebSocket', FakeWebSocketFactory)
    useAdminEventsStore.setState({ ...emptyAdminEvents })
  })
  afterEach(() => vi.unstubAllGlobals())

  it('shows the system, processor, memory, graphics and runtimes', async () => {
    serve()
    renderAt('/admin/models')

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Models & hardware' }),
    ).toBeInTheDocument()
    const hw = within(await screen.findByRole('region', { name: 'This computer' }))
    expect(await hw.findByText('darwin · arm64')).toBeInTheDocument()
    expect(hw.getByText('12 cores')).toBeInTheDocument()
    expect(hw.getByText('16 GB')).toBeInTheDocument()
    expect(hw.getAllByText('Apple M2 Pro')).toHaveLength(2) // CPU detail and GPU
    expect(hw.getByText('METAL')).toBeInTheDocument()
    expect(hw.getByText('whisper-server found')).toBeInTheDocument()
    expect(hw.getByText('Ollama found')).toBeInTheDocument()
    expect(hw.getByText('FFmpeg found')).toBeInTheDocument()
    expect(hw.getByText('Version 1.7.4')).toBeInTheDocument()
    expect(
      hw.getByText(
        'Recommended models for this computer: speech large-v3-turbo, translation gemma3:4b.',
      ),
    ).toBeInTheDocument()
    expect(hw.queryByText(/^Not running:/)).not.toBeInTheDocument()
    expect(screen.queryByText(/isn’t answering at/)).not.toBeInTheDocument()
  })

  it('warns when whisper-server and Ollama are not answering', async () => {
    serve({
      'GET /api/system/hardware': () => [
        200,
        hardware({
          whisper: { reachable: false, url: 'http://127.0.0.1:8178' },
          ollama: { reachable: false, url: 'http://127.0.0.1:11434' },
        }),
      ],
      'GET /api/models': () => [
        200,
        [
          {
            id: 'gemma3-4b',
            kind: 'gemma',
            name: 'gemma3:4b',
            status: 'error',
            error: { code: 'model.ollama_unreachable', message: 'ollama down' },
          },
        ],
      ],
    })
    renderAt('/admin/models')

    const hw = within(await screen.findByRole('region', { name: 'This computer' }))
    expect(await hw.findByText('whisper-server missing')).toBeInTheDocument()
    expect(hw.getByText('Ollama missing')).toBeInTheDocument()
    expect(hw.getByText('FFmpeg found')).toBeInTheDocument()
    expect(hw.getByText('Not answering at http://127.0.0.1:8178')).toBeInTheDocument()
    expect(hw.getByText('Not answering at http://127.0.0.1:11434')).toBeInTheDocument()
    expect(hw.getByRole('status')).toHaveTextContent('Not running: whisper-server · Ollama.')

    const models = within(await modelsRegion())
    const notice = await models.findByText(
      /whisper-server isn’t answering at http:\/\/127\.0\.0\.1:8178/,
    )
    expect(notice).toHaveTextContent('Ollama isn’t answering at http://127.0.0.1:11434')
    expect(notice).toHaveTextContent('Start them (see docs/dev.md) and check again.')
    // A Gemma model blocked on Ollama can't be downloaded, and isn't an error to fix here.
    const gemma = await row('gemma3:4b')
    expect(gemma.getByText('Needs Ollama')).toBeInTheDocument()
    expect(gemma.queryByRole('button', { name: /Download/ })).not.toBeInTheDocument()
    expect(gemma.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('lists each model with its status, the recommended flag and what is in use', async () => {
    serve()
    renderAt('/admin/models')

    const region = within(await modelsRegion())
    expect(
      await region.findByText(
        'New local sessions use speech large-v3-turbo and translation gemma3:4b.',
      ),
    ).toBeInTheDocument()

    const turbo = await row('large-v3-turbo')
    expect(turbo.getByText('Speech (whisper) · 1.6 GB · Recommended')).toBeInTheDocument()
    expect(turbo.getByText('In use')).toBeInTheDocument()
    expect(turbo.queryByText('Ready')).not.toBeInTheDocument()
    expect(turbo.queryByRole('button', { name: 'Use for new sessions' })).not.toBeInTheDocument()

    const small = await row('small')
    expect(small.getByText('Speech (whisper) · 488 MB')).toBeInTheDocument()
    expect(small.getByText('Not downloaded')).toBeInTheDocument()
    expect(small.getByRole('button', { name: 'Download' })).toBeInTheDocument()
    expect(small.queryByText('In use')).not.toBeInTheDocument()

    const gemma4 = await row('gemma3:4b')
    expect(gemma4.getByText('Translation (Gemma) · 3.3 GB · Recommended')).toBeInTheDocument()
    expect(gemma4.getByText('In use')).toBeInTheDocument()

    const gemma1 = await row('gemma3:1b')
    expect(gemma1.getByText('Ready')).toBeInTheDocument()
    expect(gemma1.getByRole('button', { name: 'Use for new sessions' })).toBeInTheDocument()

    // Every recommended model is here, so there's nothing to bulk-download.
    expect(region.queryByRole('button', { name: /recommended/ })).not.toBeInTheDocument()
  })

  it('downloads a model and follows its progress on /ws/admin until ready', async () => {
    const user = userEvent.setup()
    let models = catalog()
    const set = (m: LocalModel) => {
      models = models.map((x) => (x.id === m.id ? m : x))
    }
    const calls = serve({
      'GET /api/models': () => [200, models],
      'POST /api/models/whisper-small/download': () => {
        const m: LocalModel = { ...small, status: 'downloading', progress: 0 }
        set(m)
        return [202, m]
      },
    })
    renderAt('/admin/models')
    await row('small')
    acceptAll()
    expect(adminSockets().length).toBeGreaterThanOrEqual(1)

    await user.click((await row('small')).getByRole('button', { name: 'Download' }))
    expect(calls.filter((c) => c.method === 'POST').map((c) => c.path)).toEqual([
      '/api/models/whisper-small/download',
    ])
    expect(await (await row('small')).findByText('Downloading')).toBeInTheDocument()

    const half: LocalModel = { ...small, status: 'downloading', progress: 0.5 }
    set(half)
    push(half)
    const r = await row('small')
    const bar = await r.findByRole('progressbar', { name: 'Downloading small' })
    expect(bar).toHaveAttribute('aria-valuenow', '50')
    expect(r.getByText('244 MB of 488 MB · 50%')).toBeInTheDocument()

    const ready: LocalModel = { ...small, status: 'ready', progress: 1 }
    set(ready)
    push(ready)
    const done = await row('small')
    expect(await done.findByText('Ready')).toBeInTheDocument()
    expect(done.queryByRole('progressbar')).not.toBeInTheDocument()
    expect(done.getByRole('button', { name: 'Use for new sessions' })).toBeInTheDocument()
    // The socket is open, so the list came from events and one refetch per step.
    await waitFor(() => expect(getsOf(calls, '/api/models')).toBeGreaterThanOrEqual(3))
  })

  it('polls the list while a download runs only when the socket is down', async () => {
    const downloading: LocalModel = { ...small, status: 'downloading', progress: 0.2 }
    const calls = serve({ 'GET /api/models': () => [200, [downloading, ...catalog().slice(1)]] })
    renderAt('/admin/models')
    await row('small')
    acceptAll()
    const before = getsOf(calls, '/api/models')
    await new Promise((r) => setTimeout(r, 1800))
    expect(getsOf(calls, '/api/models')).toBe(before)

    act(() => adminSockets().forEach((ws) => ws.drop()))
    await waitFor(() => expect(getsOf(calls, '/api/models')).toBeGreaterThan(before), {
      timeout: 3000,
    })
  })

  it.each([
    { name: 'base', change: { whisperModel: 'base' }, notice: /will use base\. whisper-server/ },
    {
      name: 'gemma3:1b',
      change: { gemmaModel: 'gemma3:1b' },
      notice: /will translate with gemma3:1b\./,
    },
  ])(
    '"Use for new sessions" on $name saves only that model in the full settings',
    async ({ name, change, notice }) => {
      const user = userEvent.setup()
      let saved = settings
      const calls = serve({
        'GET /api/settings': () => [200, saved],
        'PUT /api/settings': (body) => {
          saved = body as Settings
          return [200, saved]
        },
      })
      renderAt('/admin/models')

      await user.click((await row(name)).getByRole('button', { name: 'Use for new sessions' }))

      const put = calls.find((c) => c.method === 'PUT' && c.path === '/api/settings')
      expect(put?.body).toEqual({
        ...settings,
        providers: { ...settings.providers, local: { ...settings.providers.local, ...change } },
      })
      const region = within(await modelsRegion())
      expect(await region.findByText(notice)).toBeInTheDocument()
      expect(await (await row(name)).findByText('In use')).toBeInTheDocument()
    },
  )

  it('shows a translated error when a download is refused', async () => {
    const user = userEvent.setup()
    serve({
      'POST /api/models/whisper-small/download': () => [
        409,
        { code: 'model.download_failed', message: 'upstream said no' },
      ],
    })
    renderAt('/admin/models')

    await user.click((await row('small')).getByRole('button', { name: 'Download' }))
    const alert = await within(await modelsRegion()).findByRole('alert')
    expect(alert).toHaveTextContent('The download failed')
    expect(alert).toHaveTextContent('the download resumes where it stopped')
    expect(alert).not.toHaveTextContent('upstream said no')
  })

  it('shows a translated error when the benchmark cannot run', async () => {
    const user = userEvent.setup()
    serve({
      'POST /api/system/benchmark': () => [
        422,
        {
          code: 'benchmark.runtime_unavailable',
          message: 'no',
          params: { runtime: 'Ollama' },
        },
      ],
    })
    renderAt('/admin/models')

    const hw = within(await screen.findByRole('region', { name: 'This computer' }))
    await user.click(await hw.findByRole('button', { name: 'Run benchmark' }))
    const alert = await hw.findByRole('alert')
    expect(alert).toHaveTextContent("The local AI can't run the benchmark")
    expect(alert).toHaveTextContent("Ollama isn't running or the server can't reach it.")
  })

  it('without settings it explains why models cannot be chosen', async () => {
    serve({
      'GET /api/settings': () => [500, { code: 'request.failed', message: 'boom' }],
    })
    renderAt('/admin/models')

    const region = within(await modelsRegion())
    expect(
      await region.findByText(
        'The settings couldn’t be read, so the models new sessions use can’t be changed here.',
      ),
    ).toBeInTheDocument()
    expect(region.getByRole('alert')).toBeInTheDocument()
    await row('gemma3:1b')
    expect(region.queryByRole('button', { name: 'Use for new sessions' })).not.toBeInTheDocument()
  })
})

describe('busy', () => {
  it.each([
    ['missing', false],
    ['downloading', true],
    ['verifying', true],
    ['ready', false],
    ['error', false],
  ] as const)('%s → %s', (status, want) => {
    expect(busy({ id: 'm', kind: 'whisper', name: 'm', status })).toBe(want)
  })
})
