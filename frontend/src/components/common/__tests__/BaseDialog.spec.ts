import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('BaseDialog', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('将作品预览的高度和弹性布局应用到实际面板与内容区', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: {
        show: true,
        title: '作品预览',
        contentClass: 'h-[90dvh]',
        bodyClass: 'flex min-h-0 flex-col !overflow-hidden'
      },
      slots: { default: '<div class="min-h-0 flex-1">作品</div>' },
      global: { stubs: { Icon: true } }
    })
    await nextTick()

    const panel = document.body.querySelector<HTMLElement>('.modal-content')!
    const body = panel.querySelector<HTMLElement>('.modal-body')!
    expect(panel.classList.contains('h-[90dvh]')).toBe(true)
    for (const name of ['flex', 'min-h-0', 'flex-col', '!overflow-hidden']) {
      expect(body.classList.contains(name)).toBe(true)
    }
    expect(body.parentElement).toBe(panel)
    expect(document.body.querySelector('[content-class], [body-class]')).toBeNull()

    await wrapper.setProps({ contentClass: undefined, bodyClass: undefined })
    expect(panel.classList.contains('h-[90dvh]')).toBe(false)
    expect(body.className).toBe('modal-body')
    wrapper.unmount()
  })

  it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
    wrapper.unmount()
  })
})
