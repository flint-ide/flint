// The slice of Node's test runner and assert used by *.test.ts, typed here so the
// type check needs no @types/node (and its dependency tree).

declare module 'node:test' {
  type Fn = () => void | Promise<void>
  export function describe(name: string, fn: Fn): void
  export function it(name: string, fn: Fn): void
  export function beforeEach(fn: Fn): void
  export function afterEach(fn: Fn): void
  export const mock: {
    timers: {
      enable(options?: { apis?: ('setTimeout' | 'setInterval' | 'setImmediate' | 'Date')[]; now?: number }): void
      tick(ms: number): void
      reset(): void
    }
  }
}

declare module 'node:assert/strict' {
  interface Assert {
    (value: unknown, message?: string): asserts value
    ok(value: unknown, message?: string): asserts value
    equal<T>(actual: unknown, expected: T, message?: string): asserts actual is T
    notEqual(actual: unknown, expected: unknown, message?: string): void
    deepEqual<T>(actual: unknown, expected: T, message?: string): asserts actual is T
    notDeepEqual(actual: unknown, expected: unknown, message?: string): void
    throws(fn: () => unknown, expected?: RegExp | (new (...args: never[]) => Error), message?: string): void
    match(value: string, regexp: RegExp, message?: string): void
  }
  const assert: Assert
  export default assert
}
