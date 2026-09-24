// SPDX-License-Identifier: Apache-2.0
import 'i18next'
import type admin from '../locales/en/admin.json'
import type capture from '../locales/en/capture.json'
import type common from '../locales/en/common.json'
import type overlay from '../locales/en/overlay.json'
import type replay from '../locales/en/replay.json'
import type setup from '../locales/en/setup.json'
import type stage from '../locales/en/stage.json'
import type viewer from '../locales/en/viewer.json'

// English is the reference locale for typed keys; check:i18n keeps the
// other locales in step. A new namespace needs a line here.
declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: 'common'
    resources: {
      common: typeof common
      setup: typeof setup
      admin: typeof admin
      capture: typeof capture
      viewer: typeof viewer
      stage: typeof stage
      overlay: typeof overlay
      replay: typeof replay
    }
  }
}
