import { apiClient } from '../client'

export interface IQTestAccount {
  id: number
  name: string
  type: string
  status: string
  platform: string
}

export interface IQTestResult {
  account_id: number
  account_name: string
  status: 'success' | 'failed'
  html: string
  error_message?: string
  input_tokens: number
  output_tokens: number
  total_tokens: number
  cost_usd: number
}

export interface IQTestResponse {
  model: string
  prompt: string
  results: IQTestResult[]
}

export async function listIQTestAccounts(): Promise<IQTestAccount[]> {
  const { data } = await apiClient.get<IQTestAccount[]>('/admin/iq-test/accounts')
  return data
}

export async function runIQTest(): Promise<IQTestResponse> {
  // A run can probe many accounts serially in batches; allow the server-side
  // five-minute per-account timeout to complete before Axios aborts the request.
  const { data } = await apiClient.post<IQTestResponse>('/admin/iq-test', undefined, { timeout: 10 * 60 * 1000 })
  return data
}

export default { listAccounts: listIQTestAccounts, run: runIQTest }
