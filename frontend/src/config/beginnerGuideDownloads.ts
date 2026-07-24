export interface BeginnerGuideDownload {
  label: string
  href: string
  note: string
  actionLabel: string
}

const codexDownloadBase = '/downloads/clients/codex'
const ccSwitchDownloadBase = '/downloads/clients/cc-switch'

export const codexDesktopDownloads: BeginnerGuideDownload[] = [
  {
    label: 'Windows x64',
    actionLabel: '下载 MSIX',
    href: `${codexDownloadBase}/Codex-Windows-x64.msix`,
    note: '适合绝大多数 Intel / AMD 处理器的 Windows 10/11 电脑，无需打开 Microsoft Store。'
  },
  {
    label: 'Windows ARM64',
    actionLabel: '下载 MSIX',
    href: `${codexDownloadBase}/Codex-Windows-arm64.msix`,
    note: '仅适合 Snapdragon 等 ARM 处理器的 Windows 电脑，无需打开 Microsoft Store。'
  },
  {
    label: 'macOS',
    actionLabel: '下载 DMG',
    href: `${codexDownloadBase}/Codex-macOS.dmg`,
    note: 'OpenAI 官方 macOS 安装包，由本站镜像提供下载。'
  }
]

export const ccSwitchDownloads: BeginnerGuideDownload[] = [
  {
    label: 'Windows x64 安装版',
    actionLabel: '下载 MSI',
    href: `${ccSwitchDownloadBase}/CC-Switch-Windows-x64.msi`,
    note: '推荐绝大多数 Intel / AMD 处理器的 Windows 用户使用。'
  },
  {
    label: 'Windows x64 便携版',
    actionLabel: '下载 ZIP',
    href: `${ccSwitchDownloadBase}/CC-Switch-Windows-x64-Portable.zip`,
    note: '无需安装，解压后运行，适合没有管理员权限的电脑。'
  },
  {
    label: 'Windows ARM64 安装版',
    actionLabel: '下载 MSI',
    href: `${ccSwitchDownloadBase}/CC-Switch-Windows-arm64.msi`,
    note: '仅适合 Snapdragon 等 ARM 处理器的 Windows 电脑。'
  },
  {
    label: 'macOS',
    actionLabel: '下载 DMG',
    href: `${ccSwitchDownloadBase}/CC-Switch-macOS.dmg`,
    note: '适合 macOS 图形安装，Apple Silicon 和 Intel Mac 使用同一安装包。'
  },
  {
    label: 'Linux x86_64',
    actionLabel: '下载 AppImage',
    href: `${ccSwitchDownloadBase}/CC-Switch-Linux-x86_64.AppImage`,
    note: '适合常见 Intel / AMD 64 位 Linux 桌面。'
  },
  {
    label: 'Linux ARM64',
    actionLabel: '下载 AppImage',
    href: `${ccSwitchDownloadBase}/CC-Switch-Linux-arm64.AppImage`,
    note: '适合 ARM64 Linux 桌面设备。'
  }
]
