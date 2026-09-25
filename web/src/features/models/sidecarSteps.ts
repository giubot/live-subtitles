// SPDX-License-Identifier: Apache-2.0
// The install and start steps for the local provider's sidecars
// (whisper-server and Ollama), built from the server's hardware report so
// the commands carry its real addresses, models folder and model names.
// Commands are never translated; each step's text is an i18n key under
// `models:guide.step`.

/** The tabs of the guide. Docker runs on any OS, on the CPU. */
export const guideOses = ['macos', 'linux', 'windows', 'docker'] as const
export type GuideOs = (typeof guideOses)[number]

export type Sidecar = 'whisper' | 'ollama'

export interface Addr {
  host: string
  port: string
}

export const whisperDefault: Addr = { host: '127.0.0.1', port: '8178' }
export const ollamaDefault: Addr = { host: '127.0.0.1', port: '11434' }

/** The whisper.cpp image, as in deploy/compose/compose.dev.yaml. */
const whisperImage = 'ghcr.io/ggml-org/whisper.cpp:main'

/** The tab that fits the server's GOOS; other systems get the Docker steps. */
export function osFromReport(os: string | undefined): GuideOs {
  switch (os) {
    case 'darwin':
      return 'macos'
    case 'linux':
      return 'linux'
    case 'windows':
      return 'windows'
    default:
      return 'docker'
  }
}

/** Host and port from a runtime URL such as http://127.0.0.1:8178. */
export function parseAddr(url: string | undefined, fallback: Addr): Addr {
  if (!url) return fallback
  try {
    const u = new URL(url)
    const host = u.hostname.replace(/^\[(.*)\]$/, '$1') || fallback.host
    const port = u.port || (u.protocol === 'https:' ? '443' : '80')
    return { host, port }
  } catch {
    return fallback
  }
}

/** The whisper model file on the server, with the OS's separator. */
export function whisperFile(os: GuideOs, modelsDir: string, model: string) {
  const sep = os === 'windows' ? '\\' : '/'
  const dir = modelsDir.length > 1 ? modelsDir.replace(/[\\/]+$/, '') : modelsDir
  return `${dir}${sep}ggml-${model}.bin`
}

export interface GuideInput {
  os: GuideOs
  whisper: Addr
  ollama: Addr
  /** The server's models folder, or a placeholder when it didn't say. */
  modelsDir: string
  whisperModel: string
  gemmaModel: string
  /** The sidecars that need starting. Empty means both run: show every step. */
  missing: readonly Sidecar[]
}

/** The steps' i18n keys under `models:guide.step`. */
export type StepKey =
  | `${'macos' | 'linux' | 'windows' | 'docker'}.${'ollama' | 'whisper'}`
  | 'macos.install'
  | 'windows.download'
  | 'models'
  | 'check'

export interface GuideStep {
  key: StepKey
  /** Which sidecar the step is for; model downloads and the check are for both. */
  sidecar?: Sidecar
  commands: string[]
}

const isDefaultOllama = (a: Addr) =>
  (a.host === '127.0.0.1' || a.host === 'localhost') && a.port === ollamaDefault.port

function dockerWhisper(i: GuideInput) {
  return (
    `docker run -d --name whisper-server -p ${i.whisper.port}:8178 ` +
    `-v "${i.modelsDir}:/models:ro" ${whisperImage} ` +
    `"./build/bin/whisper-server --host 0.0.0.0 --port 8178 --model /models/ggml-${i.whisperModel}.bin"`
  )
}

function dockerOllama(i: GuideInput) {
  return `docker run -d --name ollama -p ${i.ollama.port}:11434 -v ollama:/root/.ollama ollama/ollama`
}

/** The numbered steps for one OS, only for the sidecars that need them. */
export function guideSteps(i: GuideInput): GuideStep[] {
  const need = (s: Sidecar) => i.missing.length === 0 || i.missing.includes(s)
  const w = need('whisper')
  const o = need('ollama')
  const file = whisperFile(i.os, i.modelsDir, i.whisperModel)
  const nativeWhisper = `--host ${i.whisper.host} --port ${i.whisper.port} --model "${file}"`
  const steps: (GuideStep | false)[] = []

  switch (i.os) {
    case 'macos':
      steps.push(
        {
          key: 'macos.install',
          commands: [['brew install', w && 'whisper.cpp', o && 'ollama'].filter(Boolean).join(' ')],
        },
        o && {
          key: 'macos.ollama',
          sidecar: 'ollama',
          commands: [
            isDefaultOllama(i.ollama)
              ? 'ollama serve'
              : `OLLAMA_HOST=${i.ollama.host}:${i.ollama.port} ollama serve`,
          ],
        },
        { key: 'models', commands: [] },
        w && {
          key: 'macos.whisper',
          sidecar: 'whisper',
          commands: [`whisper-server ${nativeWhisper}`],
        },
      )
      break
    case 'linux':
      steps.push(
        o && {
          key: 'linux.ollama',
          sidecar: 'ollama',
          commands: ['curl -fsSL https://ollama.com/install.sh | sh'],
        },
        { key: 'models', commands: [] },
        w && { key: 'linux.whisper', sidecar: 'whisper', commands: [dockerWhisper(i)] },
      )
      break
    case 'windows':
      steps.push(
        o && {
          key: 'windows.ollama',
          sidecar: 'ollama',
          commands: ['winget install Ollama.Ollama'],
        },
        { key: 'models', commands: [] },
        w && { key: 'windows.download', sidecar: 'whisper', commands: [] },
        w && {
          key: 'windows.whisper',
          sidecar: 'whisper',
          commands: [`.\\whisper-server.exe ${nativeWhisper}`],
        },
      )
      break
    case 'docker':
      steps.push(
        o && { key: 'docker.ollama', sidecar: 'ollama', commands: [dockerOllama(i)] },
        { key: 'models', commands: [] },
        w && { key: 'docker.whisper', sidecar: 'whisper', commands: [dockerWhisper(i)] },
      )
      break
  }
  steps.push({ key: 'check', commands: [] })
  return steps.filter((s): s is GuideStep => s !== false)
}
