// SPDX-License-Identifier: Apache-2.0
import type { components } from './schema'

/** Schemas of api/openapi.yaml, by name. */
export type Schemas = components['schemas']

export type ApiErrorBody = Schemas['Error']
export type Caption = Schemas['Caption']
export type CaptionsServerMessage = Schemas['CaptionsServerMessage']
export type AdminEvent = Schemas['AdminEvent']
export type SessionStatus = Schemas['SessionStatus']
export type SessionState = Schemas['SessionState']
export type AudioSourceKind = Schemas['AudioSourceKind']
export type IngestHello = Schemas['IngestHello']
export type IngestServerMessage = Schemas['IngestServerMessage']
