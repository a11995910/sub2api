import { describe, expect, it } from 'vitest'

import { buildBatchImageListPageRequest } from '../batchImageJobsPagination'

describe('batch image job list pagination', () => {
  it('uses normal backend offset pagination for one API key', () => {
    const request = buildBatchImageListPageRequest(3, 20, 1, { status: 'completed' })

    expect(request.offset).toBe(40)
    expect(request.options).toMatchObject({
      limit: 20,
      cursor: '40',
      status: 'completed',
    })
  })

  it('fetches enough rows from every key before slicing merged pages', () => {
    const request = buildBatchImageListPageRequest(3, 20, 2, { downloaded: 'false' })

    expect(request.offset).toBe(40)
    expect(request.options).toMatchObject({
      limit: 60,
      cursor: '0',
      downloaded: 'false',
    })
  })
})

