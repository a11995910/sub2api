import { describe, expect, it } from 'vitest'
import { ccSwitchDownloads, codexDesktopDownloads } from '../beginnerGuideDownloads'

describe('小白攻略客户端下载清单', () => {
  it('Codex 桌面端全部使用本站下载路径，并覆盖 Windows 双架构和 macOS', () => {
    expect(codexDesktopDownloads.map((item) => item.label)).toEqual([
      'Windows x64',
      'Windows ARM64',
      'macOS'
    ])

    for (const item of codexDesktopDownloads) {
      expect(item.href).toMatch(/^\/downloads\/clients\/codex\//)
      expect(item.href).not.toMatch(/github\.com|microsoft\.com|oaistatic\.com/i)
    }
  })

  it('CC-Switch 全部使用本站下载路径，并覆盖三大系统和常见架构', () => {
    expect(ccSwitchDownloads.map((item) => item.label)).toEqual([
      'Windows x64 安装版',
      'Windows x64 便携版',
      'Windows ARM64 安装版',
      'macOS',
      'Linux x86_64',
      'Linux ARM64'
    ])

    for (const item of ccSwitchDownloads) {
      expect(item.href).toMatch(/^\/downloads\/clients\/cc-switch\//)
      expect(item.href).not.toContain('github.com')
    }
  })
})
