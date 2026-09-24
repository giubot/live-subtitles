// SPDX-License-Identifier: Apache-2.0

/**
 * A WebSocket stand-in for tests: records what the client sends and lets
 * the test play the server (`accept`, `receive`, `drop`).
 */
export class FakeWebSocket {
  static instances: FakeWebSocket[] = []

  /** The newest socket. */
  static get last(): FakeWebSocket {
    const ws = FakeWebSocket.instances.at(-1)
    if (!ws) throw new Error('no WebSocket was opened')
    return ws
  }

  static reset() {
    FakeWebSocket.instances = []
  }

  readonly url: string
  readyState = 0
  binaryType: BinaryType = 'blob'
  sent: unknown[] = []
  closedWith?: number
  onopen: ((ev: Event) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onclose: ((ev: CloseEvent) => void) | null = null
  onerror: ((ev: Event) => void) | null = null

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }

  send(data: unknown) {
    if (this.readyState !== 1) throw new Error('send on a socket that is not open')
    this.sent.push(data)
  }

  /** Client-side close. */
  close(code = 1000) {
    if (this.readyState === 3) return
    this.closedWith = code
    this.readyState = 3
    this.onclose?.({ code, reason: '', wasClean: true } as CloseEvent)
  }

  // Server side.

  accept() {
    this.readyState = 1
    this.onopen?.(new Event('open'))
  }

  receive(msg: unknown) {
    const data = typeof msg === 'string' || msg instanceof ArrayBuffer ? msg : JSON.stringify(msg)
    this.onmessage?.({ data } as MessageEvent)
  }

  /** The server or the network ends the connection. */
  drop(code = 1006) {
    this.readyState = 3
    this.onclose?.({ code, reason: '', wasClean: false } as CloseEvent)
  }

  /** Text frames the client sent, parsed. */
  sentJSON(): unknown[] {
    return this.sent
      .filter((d) => typeof d === 'string')
      .map((d) => JSON.parse(d as string) as unknown)
  }
}

/** The constructor to pass as the `WebSocket` option. */
export const FakeWebSocketFactory = FakeWebSocket as unknown as new (url: string) => WebSocket
