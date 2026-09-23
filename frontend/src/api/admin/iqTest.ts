import { apiClient } from '../client'

export interface IQTestAccount {
  id: number
  name: string
  type: string
  status: string
  platform: string
}

export interface IQTestModel {
  id: string
  display_name: string
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

export async function listIQTestAccounts(modelId?: string): Promise<IQTestAccount[]> {
  const { data } = await apiClient.get<IQTestAccount[]>('/admin/iq-test/accounts', {
    params: modelId ? { model_id: modelId } : undefined
  })
  return data
}

export async function listIQTestModels(): Promise<IQTestModel[]> {
  const { data } = await apiClient.get<IQTestModel[]>('/admin/iq-test/models')
  return data
}

export async function runIQTest(modelId?: string, prompt?: string): Promise<IQTestResponse> {
  // A run can probe many accounts serially in batches; allow the server-side
  // five-minute per-account timeout to complete before Axios aborts the request.
  const { data } = await apiClient.post<IQTestResponse>(
    '/admin/iq-test',
    { model_id: modelId, prompt },
    { timeout: 10 * 60 * 1000 }
  )
  return data
}

export default { listAccounts: listIQTestAccounts, listModels: listIQTestModels, run: runIQTest }
