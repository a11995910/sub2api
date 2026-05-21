<template>
  <div class="min-h-screen bg-slate-50 text-slate-900 dark:bg-dark-950 dark:text-white">
    <header class="sticky top-0 z-30 border-b border-slate-200/70 bg-white/90 px-5 py-3 backdrop-blur dark:border-dark-800 dark:bg-dark-950/90">
      <nav class="mx-auto flex max-w-6xl items-center justify-between gap-4">
        <router-link to="/home" class="flex min-w-0 items-center gap-3">
          <div class="h-10 w-10 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <div class="min-w-0">
            <p class="truncate text-sm font-semibold text-slate-900 dark:text-white">{{ siteName }}</p>
            <p class="hidden text-xs text-slate-500 dark:text-dark-400 sm:block">小白使用攻略</p>
          </div>
        </router-link>

        <div class="flex items-center gap-2">
          <LocaleSwitcher />
          <button
            type="button"
            class="rounded-lg p-2 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex items-center gap-2 rounded-lg bg-slate-950 px-3 py-2 text-sm font-medium text-white transition hover:bg-slate-800 dark:bg-white dark:text-slate-950 dark:hover:bg-slate-200"
          >
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
            <Icon name="arrowRight" size="sm" />
          </router-link>
        </div>
      </nav>
    </header>

    <main>
      <section class="border-b border-slate-200 bg-white dark:border-dark-800 dark:bg-dark-950">
        <div class="mx-auto grid max-w-6xl gap-10 px-5 py-12 lg:grid-cols-[1.1fr_0.9fr] lg:items-center lg:py-16">
          <div>
            <div class="mb-5 inline-flex items-center gap-2 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-sm font-semibold text-emerald-700 dark:border-emerald-900/70 dark:bg-emerald-950/50 dark:text-emerald-300">
              <Icon name="lightbulb" size="sm" />
              新手必看
            </div>
            <h1 class="max-w-3xl text-3xl font-bold leading-tight text-slate-950 dark:text-white sm:text-4xl lg:text-5xl">
              小白使用攻略：从新建密钥到 Codex + CC-Switch 正常使用
            </h1>
            <p class="mt-5 max-w-2xl text-base leading-7 text-slate-600 dark:text-dark-300">
              按顺序完成工具下载、创建 API 密钥、导入到 CC-Switch，再打开 Codex 使用。每一步都配了成功标志和排错入口，适合第一次配置的用户照着做。
            </p>

            <div class="mt-8 flex flex-col gap-3 sm:flex-row">
              <a href="#downloads" class="btn btn-primary justify-center rounded-lg px-5">
                <Icon name="download" size="sm" />
                先下载工具
              </a>
              <router-link to="/keys" class="btn btn-secondary justify-center rounded-lg px-5">
                <Icon name="key" size="sm" />
                已登录，去新建密钥
              </router-link>
            </div>

            <div class="mt-6 flex flex-wrap gap-3 text-xs text-slate-500 dark:text-dark-400">
              <span class="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1.5 dark:border-dark-800 dark:bg-dark-900">
                <Icon name="shield" size="xs" />
                密钥不要公开
              </span>
              <span class="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1.5 dark:border-dark-800 dark:bg-dark-900">
                <Icon name="checkCircle" size="xs" />
                推荐先装 Codex 桌面端 + CC-Switch
              </span>
            </div>
          </div>

          <div class="rounded-lg border border-slate-200 bg-slate-50 p-5 dark:border-dark-800 dark:bg-dark-900/60">
            <h2 class="mb-4 text-sm font-semibold uppercase tracking-wide text-slate-500 dark:text-dark-400">
              完整路线
            </h2>
            <ol class="space-y-4">
              <li
                v-for="(step, index) in quickSteps"
                :key="step.title"
                class="grid grid-cols-[2rem_1fr] gap-3"
              >
                <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-slate-950 text-sm font-bold text-white dark:bg-white dark:text-slate-950">
                  {{ index + 1 }}
                </span>
                <span>
                  <span class="block text-sm font-semibold text-slate-900 dark:text-white">{{ step.title }}</span>
                  <span class="mt-1 block text-sm leading-6 text-slate-600 dark:text-dark-300">{{ step.desc }}</span>
                </span>
              </li>
            </ol>
          </div>
        </div>
      </section>

      <section id="downloads" class="border-b border-slate-200 bg-slate-50 dark:border-dark-800 dark:bg-dark-950">
        <div class="mx-auto max-w-6xl px-5 py-12">
          <div class="mb-7 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
            <div>
              <p class="text-sm font-semibold text-emerald-600 dark:text-emerald-300">第 0 步</p>
              <h2 class="mt-1 text-2xl font-bold text-slate-950 dark:text-white">下载必备工具</h2>
            </div>
            <p class="max-w-xl text-sm leading-6 text-slate-600 dark:text-dark-300">
              Codex 分为 CLI 和桌面端。CLI 用统一安装方式；桌面端按系统下载。CC-Switch 全名为 cc-switch，下载入口使用 GitHub Release。
            </p>
          </div>

          <div class="grid gap-5 lg:grid-cols-3">
            <article class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm dark:border-dark-800 dark:bg-dark-900">
              <div class="mb-4 flex items-center gap-3">
                <span class="flex h-10 w-10 items-center justify-center rounded-lg bg-slate-900 text-white dark:bg-white dark:text-slate-950">
                  <Icon name="terminal" size="md" />
                </span>
                <div>
                  <h3 class="font-semibold text-slate-950 dark:text-white">Codex CLI</h3>
                  <p class="text-xs text-slate-500 dark:text-dark-400">不按设备拆分</p>
                </div>
              </div>
              <p class="text-sm leading-6 text-slate-600 dark:text-dark-300">
                适合会使用终端、需要命令行调用的用户。官方安装方式以 Codex CLI 文档为准。
              </p>
              <div class="mt-4 rounded-lg bg-slate-950 p-3 font-mono text-xs text-slate-100">
                {{ codexCliInstall }}
              </div>
              <div class="mt-4 flex flex-wrap gap-2">
                <a :href="codexCliDocsUrl" target="_blank" rel="noopener noreferrer" class="btn btn-secondary btn-sm rounded-lg">
                  官方安装说明
                  <Icon name="externalLink" size="xs" />
                </a>
                <button type="button" class="btn btn-ghost btn-sm rounded-lg" @click="copyText(codexCliInstall, 'codex-cli')">
                  <Icon :name="copiedKey === 'codex-cli' ? 'check' : 'copy'" size="xs" />
                  {{ copiedKey === 'codex-cli' ? '已复制' : '复制命令' }}
                </button>
              </div>
            </article>

            <article class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm dark:border-dark-800 dark:bg-dark-900 lg:col-span-2">
              <div class="mb-4 flex items-center justify-between gap-3">
                <div class="flex items-center gap-3">
                  <span class="flex h-10 w-10 items-center justify-center rounded-lg bg-blue-600 text-white">
                    <Icon name="download" size="md" />
                  </span>
                  <div>
                    <h3 class="font-semibold text-slate-950 dark:text-white">Codex 桌面端</h3>
                    <p class="text-xs text-slate-500 dark:text-dark-400">按系统选择</p>
                  </div>
                </div>
                <a :href="codexAppDocsUrl" target="_blank" rel="noopener noreferrer" class="hidden text-sm font-medium text-blue-600 hover:text-blue-700 dark:text-blue-300 sm:inline-flex">
                  官方说明
                </a>
              </div>
              <div class="grid gap-3 sm:grid-cols-2">
                <div
                  v-for="item in codexDesktopDownloads"
                  :key="item.system"
                  class="rounded-lg border border-slate-200 bg-slate-50 p-4 dark:border-dark-700 dark:bg-dark-950/60"
                >
                  <div class="flex items-start justify-between gap-3">
                    <div>
                      <p class="font-semibold text-slate-900 dark:text-white">{{ item.system }}</p>
                      <p class="mt-1 text-xs leading-5 text-slate-500 dark:text-dark-400">{{ item.note }}</p>
                    </div>
                    <a
                      :href="item.href"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 transition hover:border-blue-300 hover:text-blue-700 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200 dark:hover:border-blue-500 dark:hover:text-blue-300"
                    >
                      {{ item.label }}
                      <Icon name="externalLink" size="xs" />
                    </a>
                  </div>
                </div>
              </div>
            </article>
          </div>

          <article class="mt-5 rounded-lg border border-slate-200 bg-white p-5 shadow-sm dark:border-dark-800 dark:bg-dark-900">
            <div class="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div class="flex items-center gap-3">
                <span class="flex h-10 w-10 items-center justify-center rounded-lg bg-emerald-600 text-white">
                  <Icon name="sync" size="md" />
                </span>
                <div>
                  <h3 class="font-semibold text-slate-950 dark:text-white">CC-Switch（cc-switch）</h3>
                  <p class="text-xs text-slate-500 dark:text-dark-400">当前核实 Release：{{ ccSwitchVersion }}</p>
                </div>
              </div>
              <a :href="ccSwitchLatestUrl" target="_blank" rel="noopener noreferrer" class="btn btn-secondary btn-sm rounded-lg">
                GitHub 最新版本
                <Icon name="externalLink" size="xs" />
              </a>
            </div>

            <div class="grid gap-3 md:grid-cols-3">
              <div
                v-for="item in ccSwitchDownloads"
                :key="item.label"
                class="rounded-lg border border-slate-200 bg-slate-50 p-4 dark:border-dark-700 dark:bg-dark-950/60"
              >
                <p class="font-semibold text-slate-900 dark:text-white">{{ item.label }}</p>
                <p class="mt-1 min-h-10 text-xs leading-5 text-slate-500 dark:text-dark-400">{{ item.note }}</p>
                <a :href="item.href" target="_blank" rel="noopener noreferrer" class="mt-3 inline-flex items-center gap-1.5 text-sm font-semibold text-emerald-700 hover:text-emerald-800 dark:text-emerald-300">
                  GitHub 下载
                  <Icon name="download" size="xs" />
                </a>
              </div>
            </div>
          </article>
        </div>
      </section>

      <section class="border-b border-slate-200 bg-white dark:border-dark-800 dark:bg-dark-950">
        <div class="mx-auto max-w-6xl px-5 py-12">
          <div class="mb-8">
            <p class="text-sm font-semibold text-emerald-600 dark:text-emerald-300">第 1-4 步</p>
            <h2 class="mt-1 text-2xl font-bold text-slate-950 dark:text-white">从创建密钥到导入 CC-Switch</h2>
          </div>

          <div class="grid gap-5 lg:grid-cols-4">
            <article
              v-for="step in workflowCards"
              :key="step.title"
              class="rounded-lg border border-slate-200 bg-slate-50 p-5 dark:border-dark-800 dark:bg-dark-900/60"
            >
              <div class="mb-4 flex h-10 w-10 items-center justify-center rounded-lg" :class="step.iconClass">
                <Icon :name="step.icon" size="md" />
              </div>
              <h3 class="font-semibold text-slate-950 dark:text-white">{{ step.title }}</h3>
              <p class="mt-2 text-sm leading-6 text-slate-600 dark:text-dark-300">{{ step.desc }}</p>
              <p class="mt-4 rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs leading-5 text-slate-600 dark:border-dark-700 dark:bg-dark-950 dark:text-dark-300">
                <span class="font-semibold text-slate-900 dark:text-white">成功标志：</span>{{ step.success }}
              </p>
            </article>
          </div>

          <div class="mt-8 rounded-lg border border-blue-200 bg-blue-50 p-5 dark:border-blue-900/70 dark:bg-blue-950/40">
            <div class="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
              <div>
                <h3 class="font-semibold text-blue-950 dark:text-blue-100">当前站点接口地址</h3>
                <p class="mt-1 text-sm leading-6 text-blue-800 dark:text-blue-200">
                  如果需要手动配置客户端，优先使用后台“使用密钥”弹窗给出的内容；接口地址通常是当前站点地址或管理员配置的 API Base URL。
                </p>
              </div>
              <div class="flex min-w-0 items-center gap-2 rounded-lg bg-white px-3 py-2 font-mono text-xs text-blue-900 shadow-sm dark:bg-dark-900 dark:text-blue-100">
                <span class="truncate">{{ apiBaseUrl }}</span>
                <button type="button" class="rounded-md p-1 hover:bg-blue-100 dark:hover:bg-dark-800" @click="copyText(apiBaseUrl, 'api-base')">
                  <Icon :name="copiedKey === 'api-base' ? 'check' : 'copy'" size="xs" />
                </button>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section class="border-b border-slate-200 bg-slate-50 dark:border-dark-800 dark:bg-dark-950">
        <div class="mx-auto grid max-w-6xl gap-6 px-5 py-12 lg:grid-cols-[0.9fr_1.1fr]">
          <div>
            <p class="text-sm font-semibold text-emerald-600 dark:text-emerald-300">第 5 步</p>
            <h2 class="mt-1 text-2xl font-bold text-slate-950 dark:text-white">第一次运行前检查</h2>
            <p class="mt-3 text-sm leading-6 text-slate-600 dark:text-dark-300">
              完成导入后，先做一次轻量测试。能看到模型列表或正常回复，再开始正式使用。
            </p>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <div
              v-for="item in checklist"
              :key="item"
              class="flex items-start gap-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-dark-800 dark:bg-dark-900"
            >
              <Icon name="checkCircle" size="md" class="mt-0.5 shrink-0 text-emerald-600 dark:text-emerald-300" />
              <p class="text-sm leading-6 text-slate-700 dark:text-dark-200">{{ item }}</p>
            </div>
          </div>
        </div>
      </section>

      <section class="bg-white dark:bg-dark-950">
        <div class="mx-auto max-w-6xl px-5 py-12">
          <div class="mb-7">
            <p class="text-sm font-semibold text-rose-600 dark:text-rose-300">常见问题</p>
            <h2 class="mt-1 text-2xl font-bold text-slate-950 dark:text-white">卡住时先看这里</h2>
          </div>
          <div class="grid gap-4 md:grid-cols-2">
            <details
              v-for="item in faqItems"
              :key="item.q"
              class="rounded-lg border border-slate-200 bg-slate-50 p-4 dark:border-dark-800 dark:bg-dark-900/60"
            >
              <summary class="cursor-pointer list-none text-sm font-semibold text-slate-900 dark:text-white">
                {{ item.q }}
              </summary>
              <p class="mt-3 text-sm leading-6 text-slate-600 dark:text-dark-300">{{ item.a }}</p>
            </details>
          </div>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

type IconName = InstanceType<typeof Icon>['$props']['name']

interface WorkflowCard {
  title: string
  desc: string
  success: string
  icon: IconName
  iconClass: string
}

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const codexCliDocsUrl = 'https://developers.openai.com/codex/cli'
const codexAppDocsUrl = 'https://developers.openai.com/codex/app'
const codexCliInstall = 'npm install -g @openai/codex'
const ccSwitchVersion = 'v3.15.0'
const ccSwitchLatestUrl = 'https://github.com/farion1231/cc-switch/releases/latest'

const codexDesktopDownloads = [
  {
    system: 'Windows',
    label: '下载安装器',
    href: 'https://get.microsoft.com/installer/download/9PLM9XGG6VKS?cid=website_cta_psi',
    note: '适合 Windows 10/11 用户，使用 OpenAI 官方页面提供的 Microsoft 安装入口。'
  },
  {
    system: 'macOS',
    label: '下载 DMG',
    href: 'https://persistent.oaistatic.com/codex-app-prod/Codex.dmg',
    note: '适合 macOS 用户，使用 OpenAI 官方页面提供的统一 DMG。'
  },
  {
    system: 'Linux',
    label: '使用 CLI',
    href: codexCliDocsUrl,
    note: '当前官方页面未提供 Linux 桌面端下载入口，Linux 用户建议使用 Codex CLI。'
  }
]

const ccSwitchDownloads = [
  {
    label: 'Windows 安装包',
    href: 'https://github.com/farion1231/cc-switch/releases/download/v3.15.0/CC-Switch-v3.15.0-Windows.msi',
    note: '推荐大多数 Windows 用户使用。'
  },
  {
    label: 'Windows 便携版',
    href: 'https://github.com/farion1231/cc-switch/releases/download/v3.15.0/CC-Switch-v3.15.0-Windows-Portable.zip',
    note: '不想安装时使用，解压后运行。'
  },
  {
    label: 'macOS DMG',
    href: 'https://github.com/farion1231/cc-switch/releases/download/v3.15.0/CC-Switch-v3.15.0-macOS.dmg',
    note: '适合 macOS 图形安装。'
  },
  {
    label: 'Linux x86_64 AppImage',
    href: 'https://github.com/farion1231/cc-switch/releases/download/v3.15.0/CC-Switch-v3.15.0-Linux-x86_64.AppImage',
    note: '适合常见 64 位 Linux 桌面。'
  },
  {
    label: 'Linux arm64 AppImage',
    href: 'https://github.com/farion1231/cc-switch/releases/download/v3.15.0/CC-Switch-v3.15.0-Linux-arm64.AppImage',
    note: '适合 ARM64 Linux 设备。'
  },
  {
    label: 'Linux x86_64 DEB',
    href: 'https://github.com/farion1231/cc-switch/releases/download/v3.15.0/CC-Switch-v3.15.0-Linux-x86_64.deb',
    note: '适合 Debian / Ubuntu 系。'
  }
]

const quickSteps = [
  { title: '下载 Codex 和 CC-Switch', desc: 'CLI 走统一安装；桌面端按 Windows / macOS 选择。' },
  { title: '登录平台并创建 API 密钥', desc: '进入 API 密钥页，创建并妥善保存密钥。' },
  { title: '给密钥确认分组', desc: '分组决定密钥会按 OpenAI、Claude、Gemini 等哪种客户端配置。' },
  { title: '点击“导入到 CCS”', desc: '系统会调用 CC-Switch 的导入协议，把地址和密钥写入本地配置。' },
  { title: '打开 Codex 测试', desc: '确认能看到模型列表或拿到一次正常回复后再正式使用。' }
]

const workflowCards: WorkflowCard[] = [
  {
    title: '登录后进入 API 密钥',
    desc: '点击顶部控制台进入后台，再打开“API 密钥”页面。未登录用户会先进入登录页。',
    success: '看到密钥列表和“创建密钥”按钮。',
    icon: 'login',
    iconClass: 'bg-slate-900 text-white dark:bg-white dark:text-slate-950'
  },
  {
    title: '创建并保存密钥',
    desc: '点击“创建密钥”，填写名称后创建。密钥不要发给别人，截图和录屏都要打码。',
    success: '列表里出现新密钥，状态为可用。',
    icon: 'key',
    iconClass: 'bg-emerald-600 text-white'
  },
  {
    title: '检查分组',
    desc: '如果密钥没有分组，先在分组列选择可用分组。没有分组时“使用密钥”会提示先分配分组。',
    success: '密钥行能看到对应分组名称和模型类型。',
    icon: 'shield',
    iconClass: 'bg-blue-600 text-white'
  },
  {
    title: '导入到 CCS',
    desc: '先打开 CC-Switch，再回到密钥行点击“导入到 CCS”。OpenAI 分组会导入为 Codex 配置。',
    success: 'CC-Switch 弹出导入确认或配置列表出现该站点。',
    icon: 'upload',
    iconClass: 'bg-amber-500 text-white'
  }
]

const checklist = [
  'Codex 已安装；CLI 用户可在终端执行 codex 检查是否可启动。',
  'CC-Switch 已安装并能正常打开，系统协议处理程序已注册。',
  '平台 API 密钥已创建、未泄露，并且绑定了正确分组。',
  '点击“导入到 CCS”后，CC-Switch 中能看到对应 Provider。',
  '第一次测试只发简单问题，确认没有 401、403、404 或模型不可用报错。',
  '如果需要手动配置，优先复制“使用密钥”弹窗里的配置内容。'
]

const faqItems = [
  {
    q: '点击“导入到 CCS”没有反应怎么办？',
    a: '通常是 CC-Switch 未安装、没有先打开，或系统没有注册 ccswitch:// 协议。请先安装并启动 CC-Switch，再回到 API 密钥页重新点击“导入到 CCS”。'
  },
  {
    q: '提示密钥无效或 401 怎么办？',
    a: '检查密钥是否复制完整，前后是否多了空格，密钥状态是否启用，以及是否用错了其他平台的 Key。必要时重新创建一个新密钥测试。'
  },
  {
    q: 'Codex 桌面端和 CLI 应该选哪个？',
    a: '完全新手优先选择桌面端；会使用终端、需要脚本或开发环境集成时选择 CLI。两者都需要保证本地配置的接口地址和密钥正确。'
  },
  {
    q: 'Linux 为什么没有 Codex 桌面端下载？',
    a: '当前官方 Codex App 页面只提供 Windows 和 macOS 桌面端入口。Linux 用户请按官方 Codex CLI 文档安装使用。'
  },
  {
    q: '看不到“导入到 CCS”按钮怎么办？',
    a: '管理员可能在系统设置中隐藏了 CCS 导入按钮。此时可以点击“使用密钥”，按弹窗中的配置文件或环境变量方式手动配置。'
  },
  {
    q: '模型请求报 404 或模型不可用怎么办？',
    a: '优先检查密钥分组是否选对、该分组是否支持目标模型，再去“模型广场”或可用渠道页面确认当前可用模型。'
  }
]

const copiedKey = ref('')
const isDark = ref(document.documentElement.classList.contains('dark'))

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')
const isAuthenticated = computed(() => authStore.isAuthenticated)
const dashboardPath = computed(() => authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
const apiBaseUrl = computed(() => {
  const configured = appStore.cachedPublicSettings?.api_base_url
  if (configured) return configured
  return typeof window === 'undefined' ? '' : window.location.origin
})

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

async function copyText(value: string, key: string) {
  try {
    await navigator.clipboard.writeText(value)
    copiedKey.value = key
    window.setTimeout(() => {
      if (copiedKey.value === key) copiedKey.value = ''
    }, 1800)
  } catch {
    copiedKey.value = ''
  }
}

onMounted(() => {
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>
