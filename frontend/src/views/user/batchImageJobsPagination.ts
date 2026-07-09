import type { BatchImageJobsListOptions } from '@/api/batchImage'

export interface BatchImageListPageRequest {
  options: BatchImageJobsListOptions
  offset: number
}

export function buildBatchImageListPageRequest(
  page: number,
  pageSize: number,
  keyCount: number,
  baseOptions: Omit<BatchImageJobsListOptions, 'limit' | 'cursor'> = {},
): BatchImageListPageRequest {
  const normalizedPage = Math.max(1, Math.floor(Number(page) || 1))
  const normalizedPageSize = Math.max(1, Math.floor(Number(pageSize) || 1))
  const offset = (normalizedPage - 1) * normalizedPageSize
  const needsMergedPagination = keyCount > 1
  const limit = needsMergedPagination ? normalizedPage * normalizedPageSize : normalizedPageSize
  const cursor = needsMergedPagination ? 0 : offset

  return {
    options: {
      ...baseOptions,
      limit,
      cursor: String(cursor),
    },
    offset,
  }
}

