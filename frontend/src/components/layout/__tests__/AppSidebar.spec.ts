import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

describe('AppSidebar 自定义菜单顺序', () => {
  it('将用户侧自定义菜单整体放在模型广场之后、模型测试台之前', () => {
    const modelMarketIndex = componentSource.indexOf("path: '/models'")
    const customMenuIndex = componentSource.indexOf('...customMenuItemsForUser.value.map(customMenuToNavItem)')
    const modelTestIndex = componentSource.indexOf("path: '/model-test'")

    expect(modelMarketIndex).toBeGreaterThanOrEqual(0)
    expect(customMenuIndex).toBeGreaterThan(modelMarketIndex)
    expect(modelTestIndex).toBeGreaterThan(customMenuIndex)
    expect(componentSource).not.toContain("path: '/creative-drawing'")
  })

  it('将管理员自定义菜单放在系统设置之前', () => {
    const adminCustomMenuIndex = componentSource.indexOf('for (const cm of customMenuItemsForAdmin.value)')
    const adminSettingsIndex = componentSource.indexOf("visible.push({ path: '/admin/settings'")

    expect(adminCustomMenuIndex).toBeGreaterThanOrEqual(0)
    expect(adminSettingsIndex).toBeGreaterThan(adminCustomMenuIndex)
  })
})
