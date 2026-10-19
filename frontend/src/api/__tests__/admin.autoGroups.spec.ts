import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: mocks
}))

import { autoGroupsAPI, type CreateAutoGroupProfileRequest } from '@/api/admin/autoGroups'

const filter = {
  enabled: true,
  models: ['gpt-*', 'claude-*']
}

const request: CreateAutoGroupProfileRequest = {
  name: 'Primary Auto',
  status: 'active',
  max_candidates: 3,
  allow_custom_order: true,
  cross_group_retry_allowed: false,
  group_ids: [11, 12],
  model_catalog_filter: filter
}

describe('admin Auto Groups API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockResolvedValue({ data: [] })
    mocks.post.mockResolvedValue({ data: { ...request, id: 3, revision: 1 } })
    mocks.put.mockResolvedValue({ data: { ...request, id: 3, revision: 2 } })
  })

  it('accepts legacy profile responses without a model catalog filter', async () => {
    mocks.get.mockResolvedValue({
      data: [{
        id: 3,
        name: request.name,
        status: request.status,
        revision: 1,
        max_candidates: request.max_candidates,
        allow_custom_order: request.allow_custom_order,
        cross_group_retry_allowed: request.cross_group_retry_allowed,
        group_ids: request.group_ids
      }]
    })

    const [profile] = await autoGroupsAPI.list()

    expect(profile.model_catalog_filter).toBeUndefined()
  })

  it('sends the model catalog filter when creating and updating a profile', async () => {
    await autoGroupsAPI.create(request)
    await autoGroupsAPI.update(3, { ...request, revision: 1 })

    expect(mocks.post).toHaveBeenCalledWith('/admin/auto-group-profiles', request)
    expect(mocks.put).toHaveBeenCalledWith('/admin/auto-group-profiles/3', {
      ...request,
      revision: 1
    })
  })

  it('fetches models for the selected groups without requiring a saved profile', async () => {
    mocks.post.mockResolvedValueOnce({ data: { models: ['private-model', 'claude-local'] } })

    expect(await autoGroupsAPI.getModelCatalog([12, 11])).toEqual(['private-model', 'claude-local'])
    expect(mocks.post).toHaveBeenCalledWith('/admin/auto-group-profiles/model-catalog', {
      group_ids: [12, 11]
    })
  })
})
