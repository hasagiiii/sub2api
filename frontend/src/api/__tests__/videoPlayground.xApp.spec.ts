import axios, { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { describe, expect, it, vi } from 'vitest'

describe('video playground application header', () => {
  it('marks submission, polling, results and estimates without affecting other clients', async () => {
    const requests: InternalAxiosRequestConfig[] = []
    const create = axios.create.bind(axios)
    const spy = vi.spyOn(axios, 'create').mockImplementation(config => create({
      ...config,
      adapter: async request => {
        requests.push(request)
        return { data: {}, status: 200, statusText: 'OK', headers: {}, config: request }
      }
    }))
    try {
      const api = await import('../videoPlayground')
      await api.submit('image/model', { prompt: 'draw' }, 'test-key', { internalRequestId: 'request-1' })
      await api.status('image/model', 'request-1', 'test-key')
      await api.result('image/model', 'request-1', 'test-key')
      await api.estimateImage('image/model', { num_images: 2 }, 'test-key')
      expect(requests).toHaveLength(4)
      for (const request of requests) {
        expect(request.headers.get('x-app')).toBe('opentk')
        expect(request.headers.get('Authorization')).toBe('Bearer test-key')
      }
      expect(requests[0].headers.get('x-client-request-id')).toBe('request-1')
      expect(AxiosHeaders.from(axios.defaults.headers.common).has('x-app')).toBe(false)
    } finally {
      spy.mockRestore()
    }
  })
})
