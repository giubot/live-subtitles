// SPDX-License-Identifier: Apache-2.0
import type { i18n as I18n } from 'i18next'

/** The API's Error body (api/openapi.yaml § Error), or anything shaped like it. */
export interface ApiErrorLike {
  code: string
  message?: string
  params?: Record<string, unknown>
}

/** Client-side code for a request that never got an answer. */
export const networkErrorCode = 'network.unreachable'

/** Codes are dotted snake_case (`session.not_found`); anything else is not a key we'd look up. */
const codePattern = /^[a-z0-9_]+(\.[a-z0-9_]+)*$/

/** Pulls a translatable error out of whatever a request threw or returned. */
export function toApiError(err: unknown): ApiErrorLike | undefined {
  if (err && typeof err === 'object' && 'code' in err && typeof err.code === 'string') {
    const { code, message, params } = err as ApiErrorLike
    return {
      code,
      message: typeof message === 'string' ? message : undefined,
      params: params && typeof params === 'object' ? params : undefined,
    }
  }
  // fetch() rejects with a TypeError when the server can't be reached.
  if (err instanceof TypeError && /fetch|network|load failed/i.test(err.message)) {
    return { code: networkErrorCode }
  }
  return undefined
}

export interface DescribedError {
  /** What broke. */
  title: string
  /** Why it broke. */
  why: string
  /** What to do about it. */
  fix: string
  /** The raw code, when there is one. */
  code?: string
  /** False when the code has no translation and the generic text is used. */
  known: boolean
}

/**
 * Translates an API error code into "what broke, why, what to do"
 * (docs/design.md § Copy, UI-4). Keys live in common.json under
 * `errors.<code>.{title,why,fix}`; `params` fill the placeholders. Unknown
 * codes get the generic text, and the caller shows the code.
 */
export function describeError(i18n: I18n, err: unknown): DescribedError {
  const api = toApiError(err)
  const t = i18n.getFixedT(null, 'common')
  const code = api?.code
  const values = { ...api?.params, code: code ?? '' }
  const base = code && codePattern.test(code) ? `errors.${code}` : undefined
  if (base && i18n.exists(`${base}.title`, { ns: 'common' })) {
    // Dynamic keys: the code comes from the server, so typed keys can't cover it.
    const tk = t as unknown as (key: string, options?: Record<string, unknown>) => string
    return {
      title: tk(`${base}.title`, values),
      why: tk(`${base}.why`, values),
      fix: tk(`${base}.fix`, values),
      code,
      known: true,
    }
  }
  return {
    title: t('errors.generic.title'),
    why: t('errors.generic.why'),
    fix: t('errors.generic.fix'),
    code,
    known: false,
  }
}
