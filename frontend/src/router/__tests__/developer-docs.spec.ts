import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const testDir = dirname(fileURLToPath(import.meta.url))
const routerSource = readFileSync(resolve(testDir, '../index.ts'), 'utf8')
const headerSource = readFileSync(resolve(testDir, '../../components/layout/AppHeader.vue'), 'utf8')
const sidebarSource = readFileSync(resolve(testDir, '../../components/layout/AppSidebar.vue'), 'utf8')
const adminApiSource = readFileSync(resolve(testDir, '../../api/admin/index.ts'), 'utf8')

describe('开发文档入口', () => {
  it('注册所有登录用户可访问的独立路由', () => {
    expect(routerSource).toContain("path: '/developer-docs'")
    expect(routerSource).toContain("name: 'DeveloperDocs'")
    expect(routerSource).toContain("component: () => import('@/views/user/DeveloperDocsView.vue')")
  })

  it('将入口放在公告按钮左侧，并提供窄屏菜单入口', () => {
    const docsIndex = headerSource.indexOf('to="/developer-docs"')
    const announcementIndex = headerSource.indexOf('<AnnouncementBell')

    expect(docsIndex).toBeGreaterThanOrEqual(0)
    expect(announcementIndex).toBeGreaterThan(docsIndex)
    expect(headerSource.match(/to="\/developer-docs"/g)).toHaveLength(2)
  })
})

describe('上游倍率功能页删除', () => {
  it('不再注册旧路由、侧边栏入口或前端 API', () => {
    expect(routerSource).not.toContain('/admin/upstream-rate-monitors')
    expect(sidebarSource).not.toContain('/admin/upstream-rate-monitors')
    expect(adminApiSource).not.toContain('upstreamRateMonitor')
  })
})
