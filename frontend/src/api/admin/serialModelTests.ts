import { apiClient, buildApiUrl } from '../client'
import { getById } from './accounts'

export interface SerialModelResult {
  model: string
  actual_model?: string
  status: 'pending' | 'running' | 'success' | 'failed' | 'skipped'
  error?: string
  latency_ms: number
}

export interface SerialTestRun {
  started_at: string
  finished_at?: string
  status: 'running' | 'completed' | 'cancelled'
  results: SerialModelResult[]
}

export const parseModelList = (text: string) => [...new Set(text.split(/\r?\n/).map(s => s.trim()).filter(Boolean))]

export async function getLatestSerialTest(id: number): Promise<SerialTestRun | null> {
  const account = await getById(id)
  return (account.extra?.latest_serial_model_test as SerialTestRun | undefined) ?? null
}

export async function getSerialPreset(id: number): Promise<string[]> {
  const { data } = await apiClient.get<{ models: string[] }>(`/admin/accounts/${id}/test-models/preset`)
  return data.models
}

export async function saveSerialPreset(id: number, models: string[]): Promise<void> {
  await apiClient.put(`/admin/accounts/${id}/test-models/preset`, { models })
}

export async function runSerialTest(id: number, models: string[], signal: AbortSignal, onResult: (run: SerialTestRun) => void): Promise<void> {
  const response = await fetch(buildApiUrl(`/admin/accounts/${id}/test-models`), {
    method: 'POST', credentials: 'include', signal,
    headers: { Authorization: `Bearer ${localStorage.getItem('auth_token')}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ models })
  })
  if (!response.ok) {
    const body = await response.json().catch(() => null)
    throw new Error(body?.message || `HTTP ${response.status}`)
  }
  const reader = response.body?.getReader()
  if (!reader) throw new Error('No response stream')
  const decoder = new TextDecoder()
  let buffer = ''
  let completed = false
  const consume = (line: string) => {
    if (!line.startsWith('data:')) return
    const event = JSON.parse(line.slice(5).trim())
    if (event.type === 'error') throw new Error(String(event.data))
    if (event.type === 'snapshot' || event.type === 'complete') onResult(event.data)
    if (event.type === 'complete') completed = true
  }
  try {
    while (true) {
      const { done, value } = await reader.read()
      buffer += decoder.decode(value, { stream: !done })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''
      lines.forEach(consume)
      if (done) { if (buffer.trim()) consume(buffer); break }
    }
    if (!completed) throw new Error('Test stream disconnected before results were saved')
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}

// Preserve untouched rules; explicit entries take precedence over wildcard rules.
export function remapTestedModels(existing: { from: string; to: string }[], sources: string[], target: string) {
  const entries = new Map(existing.map(item => [item.from, item.to]))
  for (const source of sources) entries.set(source, target)
  entries.set(target, target)
  return Array.from(entries, ([from, to]) => ({ from, to }))
}
