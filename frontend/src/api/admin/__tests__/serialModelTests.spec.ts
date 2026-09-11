import { afterEach, describe, expect, it, vi } from 'vitest'
import { parseModelList, remapTestedModels, runSerialTest } from '../serialModelTests'

vi.mock('../accounts', () => ({ getById: vi.fn() }))
vi.mock('../../client', () => ({ apiClient: {}, buildApiUrl: (path: string) => path }))

afterEach(() => vi.unstubAllGlobals())

describe('serial model tests', () => {
  it('normalizes ordered models', () => {
    expect(parseModelList(' a\r\nb\n a \n')).toEqual(['a', 'b'])
  })
  it('preserves unrelated mappings and makes the tested target an identity entry', () => {
    expect(remapTestedModels([{ from: '*', to: 'old' }, { from: 'good', to: 'bad' }], ['failed'], 'good'))
      .toEqual([{ from: '*', to: 'old' }, { from: 'good', to: 'good' }, { from: 'failed', to: 'good' }])
  })
  it('parses split events and requires persisted completion', async () => {
    const update = vi.fn()
    const encoder = new TextEncoder()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(new ReadableStream({
      start(controller) {
        controller.enqueue(encoder.encode('data:{"type":"snap'))
        controller.enqueue(encoder.encode('shot","data":{"status":"running"}}\r\n\r\ndata:{"type":"complete","data":{"status":"completed"}}'))
        controller.close()
      }
    }))))
    await runSerialTest(1, ['a'], new AbortController().signal, update)
    expect(update).toHaveBeenLastCalledWith({ status: 'completed' })
  })
  it('does not treat an incomplete stream as success', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('data:{"type":"heartbeat"}\n\n')))
    await expect(runSerialTest(1, ['a'], new AbortController().signal, vi.fn())).rejects.toThrow('disconnected')
  })
  it('surfaces persistence errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('data:{"type":"error","data":"save failed"}\n\n')))
    await expect(runSerialTest(1, ['a'], new AbortController().signal, vi.fn())).rejects.toThrow('save failed')
  })
})
