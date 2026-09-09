export interface ModelAllowlistConfig {
  enabled: boolean
  models: string[]
}

export interface ModelAllowlistItem {
  id: string
  selected: boolean
}

export interface ModelAllowlistState {
  enabled: boolean
  draft: string
  savedModels: string[]
  items: ModelAllowlistItem[]
}

// Compatibility aliases for the older view bindings retained in this branch.
export type ModelsListConfig = ModelAllowlistConfig
export type ModelsListItem = ModelAllowlistItem
export type ModelsListState = ModelAllowlistState

// 自定义条目校验错误码，由视图映射为 i18n 提示。
export type ModelAllowlistAddError = 'empty' | 'invalid_wildcard' | 'duplicate'

export const createModelAllowlistState = (
  config?: Partial<ModelAllowlistConfig> | null,
): ModelAllowlistState => ({
  enabled: config?.enabled ?? false,
  draft: '',
  savedModels: normalizeModels(config?.models ?? []),
  items: [],
})

export const filterModelsListItems = (
  items: ModelsListItem[],
  search: string,
): ModelsListItem[] => {
  const query = search.trim().toLowerCase()
  if (!query) {
    return items
  }
  return items.filter(item => item.id.toLowerCase().includes(query))
}

export const hydrateModelsListState = (
  config: Partial<ModelAllowlistConfig> | null | undefined,
  candidates: string[],
): ModelAllowlistState => {
  const state = createModelAllowlistState(config)
  setModelAllowlistCandidates(state, candidates)
  return state
}

export const setModelAllowlistCandidates = (
  state: ModelAllowlistState,
  candidates: string[],
) => {
  const normalizedCandidates = normalizeModels(candidates)
  const currentSelected = new Set(
    state.items.filter(item => item.selected).map(item => item.id),
  )
  const currentKnown = new Set(state.items.map(item => item.id))
  const savedSelected = new Set(state.savedModels)
  const hasExistingItems = state.items.length > 0
  const selectionOrder = normalizeModels([
    ...state.items.map(item => item.id),
    ...state.savedModels,
    ...normalizedCandidates,
  ])

  state.items = selectionOrder.map(id => {
    const selected = hasExistingItems
      ? currentSelected.has(id)
      : state.savedModels.length > 0
        ? savedSelected.has(id)
        : normalizedCandidates.includes(id)

    return {
      id,
      selected: selected && (currentKnown.has(id) || savedSelected.has(id) || state.savedModels.length === 0),
    }
  })
}

export const toggleModelAllowlistItem = (
  state: ModelAllowlistState,
  modelID: string,
) => {
  const item = state.items.find(item => item.id === modelID)
  if (item) {
    item.selected = !item.selected
  }
}

export const addModelsListItems = (
  state: ModelAllowlistState,
  input: string | string[],
) => {
  const models = normalizeModels(
    Array.isArray(input) ? input : input.split(/[,;\n]+/),
  )
  if (models.length === 0) {
    return
  }

  if (state.items.length === 0 && state.savedModels.length > 0) {
    state.items = state.savedModels.map(id => ({ id, selected: true }))
  }

  const itemsByID = new Map(state.items.map(item => [item.id, item]))
  for (const id of models) {
    const existing = itemsByID.get(id)
    if (existing) {
      existing.selected = true
      continue
    }
    const item = { id, selected: true }
    state.items.push(item)
    itemsByID.set(id, item)
  }
}

export const selectAllModelsListItems = (state: ModelAllowlistState) => {
  state.items.forEach(item => {
    item.selected = true
  })
}

export const invertModelAllowlistSelection = (state: ModelAllowlistState) => {
  state.items.forEach(item => {
    item.selected = !item.selected
  })
}

export const moveModelAllowlistItem = (
  state: ModelAllowlistState,
  fromIndex: number,
  toIndex: number,
) => {
  if (
    fromIndex === toIndex ||
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= state.items.length ||
    toIndex >= state.items.length
  ) {
    return
  }
  const [item] = state.items.splice(fromIndex, 1)
  state.items.splice(toIndex, 0, item)
}

// addCustomModelAllowlistItem 把手工输入的条目追加到白名单末尾（选中状态）。
// 去重；`*` 只允许出现在末尾。返回错误码或 null（成功）。
export const addCustomModelAllowlistItem = (
  state: ModelAllowlistState,
  raw: string,
): ModelAllowlistAddError | null => {
  const entry = raw.trim()
  if (!entry) {
    return 'empty'
  }
  if (entry.slice(0, -1).includes('*')) {
    return 'invalid_wildcard'
  }
  if (
    state.items.some(item => item.id.toLowerCase() === entry.toLowerCase()) ||
    state.savedModels.some(model => model.toLowerCase() === entry.toLowerCase())
  ) {
    return 'duplicate'
  }
  state.items.push({ id: entry, selected: true })
  return null
}

export const buildModelAllowlistConfig = (
  state: ModelAllowlistState,
): ModelAllowlistConfig => ({
  enabled: state.enabled,
  models: state.items.length > 0
    ? state.items.filter(item => item.selected).map(item => item.id)
    : [...state.savedModels],
})

export const createModelsListState = createModelAllowlistState
export const setModelsListCandidates = setModelAllowlistCandidates
export const toggleModelsListItem = toggleModelAllowlistItem
export const selectAllModelAllowlistItems = selectAllModelsListItems
export const buildModelsListConfig = buildModelAllowlistConfig
export const moveModelsListItem = moveModelAllowlistItem

export const selectedModelAllowlistCount = (state: ModelAllowlistState): number =>
  state.items.filter(item => item.selected).length

const normalizeModels = (models: string[]): string[] => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of models) {
    const model = raw.trim()
    if (!model || seen.has(model)) {
      continue
    }
    seen.add(model)
    out.push(model)
  }
  return out
}
