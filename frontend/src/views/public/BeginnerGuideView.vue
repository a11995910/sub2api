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
        <div class="mx-auto max-w-6xl px-5 pt-5">
          <div class="overflow-x-auto">
            <div class="inline-flex min-w-full gap-1 rounded-lg border border-slate-200 bg-slate-50 p-1 dark:border-dark-800 dark:bg-dark-900 sm:min-w-0">
              <button
                v-for="tab in guideTabs"
                :key="tab.key"
                type="button"
                class="inline-flex min-w-[7.5rem] flex-1 items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors sm:flex-none"
                :class="activeGuideKey === tab.key
                  ? 'bg-white text-slate-950 shadow-sm dark:bg-dark-800 dark:text-white'
                  : 'text-slate-600 hover:text-slate-950 dark:text-dark-300 dark:hover:text-white'"
                @click="activeGuideKey = tab.key"
              >
                <Icon :name="tab.icon" size="sm" />
                {{ tab.label }}
              </button>
            </div>
          </div>
        </div>
      </section>

      <section class="border-b border-slate-200 bg-white dark:border-dark-800 dark:bg-dark-950">
        <div class="mx-auto grid max-w-6xl gap-10 px-5 py-12 lg:grid-cols-[1.1fr_0.9fr] lg:items-center lg:py-16">
          <div>
            <div
              class="mb-5 inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-sm font-semibold"
              :class="activeGuide.hero.badgeClass"
            >
              <Icon name="lightbulb" size="sm" />
              {{ activeGuide.hero.badge }}
            </div>
            <h1 class="max-w-3xl text-balance text-3xl font-semibold tracking-tight text-slate-950 dark:text-white sm:text-4xl lg:text-5xl">
              {{ activeGuide.hero.title }}
            </h1>
            <p class="mt-5 max-w-[70ch] text-pretty text-base leading-7 text-slate-600 dark:text-dark-300">
              {{ activeGuide.hero.desc }}
            </p>

            <div class="mt-8 flex flex-col gap-3 sm:flex-row">
              <a href="#downloads" class="btn btn-primary justify-center rounded-lg px-5" @click="activeGuideKey = activeGuide.key">
                <Icon name="download" size="sm" />
                {{ activeGuide.hero.primaryAction }}
              </a>
              <router-link
                :to="activeGuide.hero.secondaryAction.to"
                class="btn btn-secondary justify-center rounded-lg px-5"
              >
                <Icon :name="activeGuide.hero.secondaryAction.icon" size="sm" />
                {{ activeGuide.hero.secondaryAction.label }}
              </router-link>
            </div>

            <div class="mt-6 flex flex-wrap gap-3 text-xs text-slate-500 dark:text-dark-400">
              <span
                v-for="tag in activeGuide.hero.tags"
                :key="tag.label"
                class="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1.5 dark:border-dark-800 dark:bg-dark-900"
              >
                <Icon :name="tag.icon" size="xs" />
                {{ tag.label }}
              </span>
            </div>
          </div>

          <div class="rounded-lg border border-slate-200 bg-slate-50 p-5 dark:border-dark-800 dark:bg-dark-900/60">
            <h2 class="mb-4 font-mono text-sm font-semibold uppercase tracking-wide text-slate-500 dark:text-dark-400">
              完整路线
            </h2>
            <ol class="space-y-4">
              <li
                v-for="(step, index) in activeGuide.quickSteps"
                :key="step.title"
                class="grid grid-cols-[2rem_1fr] gap-3"
              >
                <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-slate-950 text-sm font-semibold text-white dark:bg-white dark:text-slate-950">
                  {{ index + 1 }}
                </span>
                <span>
                  <span class="block text-base font-semibold text-slate-900 dark:text-white sm:text-sm">{{ step.title }}</span>
                  <span class="mt-1 block text-base leading-7 text-slate-600 dark:text-dark-300 sm:text-sm sm:leading-6">{{ step.desc }}</span>
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
              <h2 class="mt-1 text-2xl font-semibold tracking-tight text-slate-950 dark:text-white">{{ activeGuide.downloadsTitle }}</h2>
            </div>
            <p class="max-w-xl text-pretty text-base leading-7 text-slate-600 dark:text-dark-300 sm:text-sm sm:leading-6">
              {{ activeGuide.downloadsDesc }}
            </p>
          </div>

          <div class="grid gap-5 lg:grid-cols-3">
            <article
              v-for="card in activeGuide.downloadCards"
              :key="card.title"
              class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm dark:border-dark-800 dark:bg-dark-900"
              :class="card.wide ? 'lg:col-span-2' : ''"
            >
              <div class="mb-4 flex items-center gap-3">
                <span class="flex h-10 w-10 items-center justify-center rounded-lg" :class="card.iconClass">
                  <Icon :name="card.icon" size="md" />
                </span>
                <div>
                  <h3 class="font-semibold text-slate-950 dark:text-white">{{ card.title }}</h3>
                  <p class="text-xs text-slate-500 dark:text-dark-400">{{ card.kicker }}</p>
                </div>
              </div>
              <p class="text-pretty text-base leading-7 text-slate-600 dark:text-dark-300 sm:text-sm sm:leading-6">
                {{ card.desc }}
              </p>

              <div v-if="card.command" class="mt-4 rounded-lg bg-slate-950 p-3 font-mono text-xs leading-5 text-slate-100">
                {{ card.command }}
              </div>

              <div v-if="card.items" class="mt-4 grid gap-3 sm:grid-cols-2">
                <div
                  v-for="item in card.items"
                  :key="item.label"
                  class="rounded-lg border border-slate-200 bg-slate-50 p-4 dark:border-dark-700 dark:bg-dark-950/60"
                >
                  <div class="flex items-start justify-between gap-3">
                    <div>
                      <p class="font-semibold text-slate-900 dark:text-white">{{ item.label }}</p>
                      <p class="mt-1 text-sm leading-6 text-slate-500 dark:text-dark-400 sm:text-xs sm:leading-5">{{ item.note }}</p>
                    </div>
                    <a
                      :href="item.href"
                      :target="item.external ? '_blank' : undefined"
                      :rel="item.external ? 'noopener noreferrer' : undefined"
                      class="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 transition hover:border-blue-300 hover:text-blue-700 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200 dark:hover:border-blue-500 dark:hover:text-blue-300"
                    >
                      {{ item.actionLabel }}
                      <Icon :name="item.external ? 'externalLink' : 'download'" size="xs" />
                    </a>
                  </div>
                </div>
              </div>

              <div class="mt-4 flex flex-wrap gap-2">
                <a
                  v-for="action in card.actions"
                  :key="action.label"
                  :href="action.href"
                  :target="action.external ? '_blank' : undefined"
                  :rel="action.external ? 'noopener noreferrer' : undefined"
                  class="btn btn-secondary btn-sm rounded-lg"
                >
                  {{ action.label }}
                  <Icon :name="action.external ? 'externalLink' : 'arrowRight'" size="xs" />
                </a>
                <button
                  v-if="card.command"
                  type="button"
                  class="btn btn-ghost btn-sm rounded-lg"
                  @click="copyText(card.command, card.copyKey || card.title)"
                >
                  <Icon :name="copiedKey === (card.copyKey || card.title) ? 'check' : 'copy'" size="xs" />
                  {{ copiedKey === (card.copyKey || card.title) ? '已复制' : '复制命令' }}
                </button>
              </div>
            </article>
          </div>

          <article
            v-if="activeGuide.showCcSwitch"
            class="mt-5 rounded-lg border border-slate-200 bg-white p-5 shadow-sm dark:border-dark-800 dark:bg-dark-900"
          >
            <div class="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div class="flex items-center gap-3">
                <span class="flex h-10 w-10 items-center justify-center rounded-lg bg-emerald-600 text-white">
                  <Icon name="sync" size="md" />
                </span>
                <div>
                  <h3 class="font-semibold text-slate-950 dark:text-white">CC-Switch（cc-switch）</h3>
                  <p class="text-xs text-slate-500 dark:text-dark-400">本站直连 · 官方原版安装包</p>
                </div>
              </div>
              <a href="/downloads/clients/SHA256SUMS.txt" class="btn btn-secondary btn-sm rounded-lg">
                文件校验值
                <Icon name="download" size="xs" />
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
                <a :href="item.href" class="mt-3 inline-flex items-center gap-1.5 text-sm font-semibold text-emerald-700 hover:text-emerald-800 dark:text-emerald-300">
                  {{ item.actionLabel }}
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
            <h2 class="mt-1 text-2xl font-semibold tracking-tight text-slate-950 dark:text-white">{{ activeGuide.workflowTitle }}</h2>
          </div>

          <div class="grid gap-5 lg:grid-cols-4">
            <article
              v-for="step in activeGuide.workflowCards"
              :key="step.title"
              class="rounded-lg border border-slate-200 bg-slate-50 p-5 dark:border-dark-800 dark:bg-dark-900/60"
            >
              <div class="mb-4 flex h-10 w-10 items-center justify-center rounded-lg" :class="step.iconClass">
                <Icon :name="step.icon" size="md" />
              </div>
              <h3 class="font-semibold text-slate-950 dark:text-white">{{ step.title }}</h3>
              <p class="mt-2 text-pretty text-base leading-7 text-slate-600 dark:text-dark-300 sm:text-sm sm:leading-6">{{ step.desc }}</p>
              <p class="mt-4 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm leading-6 text-slate-600 dark:border-dark-700 dark:bg-dark-950 dark:text-dark-300 sm:text-xs sm:leading-5">
                <span class="font-semibold text-slate-900 dark:text-white">成功标志：</span>{{ step.success }}
              </p>
            </article>
          </div>

          <div class="mt-8 rounded-lg border border-blue-200 bg-blue-50 p-5 dark:border-blue-900/70 dark:bg-blue-950/40">
            <div class="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
              <div>
                <h3 class="font-semibold text-blue-950 dark:text-blue-100">当前站点接口地址</h3>
                <p class="mt-1 text-base leading-7 text-blue-800 dark:text-blue-200 sm:text-sm sm:leading-6">
                  {{ activeGuide.apiBaseHint }}
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
            <h2 class="mt-1 text-2xl font-semibold tracking-tight text-slate-950 dark:text-white">{{ activeGuide.checkTitle }}</h2>
            <p class="mt-3 text-pretty text-base leading-7 text-slate-600 dark:text-dark-300 sm:text-sm sm:leading-6">
              {{ activeGuide.checkDesc }}
            </p>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <div
              v-for="item in activeGuide.checklist"
              :key="item"
              class="flex items-start gap-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-dark-800 dark:bg-dark-900"
            >
              <Icon name="checkCircle" size="md" class="mt-0.5 shrink-0 text-emerald-600 dark:text-emerald-300" />
              <p class="text-base leading-7 text-slate-700 dark:text-dark-200 sm:text-sm sm:leading-6">{{ item }}</p>
            </div>
          </div>
        </div>
      </section>

      <section class="bg-white dark:bg-dark-950">
        <div class="mx-auto max-w-6xl px-5 py-12">
          <div class="mb-7">
            <p class="text-sm font-semibold text-rose-600 dark:text-rose-300">常见问题</p>
            <h2 class="mt-1 text-2xl font-semibold tracking-tight text-slate-950 dark:text-white">卡住时先看这里</h2>
          </div>
          <div class="grid gap-4 md:grid-cols-2">
            <details
              v-for="item in activeGuide.faqItems"
              :key="item.q"
              class="rounded-lg border border-slate-200 bg-slate-50 p-4 dark:border-dark-800 dark:bg-dark-900/60"
            >
              <summary class="cursor-pointer list-none text-base font-semibold text-slate-900 dark:text-white sm:text-sm">
                {{ item.q }}
              </summary>
              <p class="mt-3 text-base leading-7 text-slate-600 dark:text-dark-300 sm:text-sm sm:leading-6">{{ item.a }}</p>
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
import { ccSwitchDownloads, codexDesktopDownloads } from '@/config/beginnerGuideDownloads'

type IconName = InstanceType<typeof Icon>['$props']['name']

interface WorkflowCard {
  title: string
  desc: string
  success: string
  icon: IconName
  iconClass: string
}

type GuideKey = 'codex' | 'claudecode' | 'image'

interface DownloadItem {
  label: string
  href: string
  note: string
  actionLabel: string
  external?: boolean
}

interface DownloadAction {
  label: string
  href: string
  external?: boolean
}

interface DownloadCard {
  title: string
  kicker: string
  desc: string
  icon: IconName
  iconClass: string
  command?: string
  copyKey?: string
  wide?: boolean
  items?: DownloadItem[]
  actions: DownloadAction[]
}

interface GuideTab {
  key: GuideKey
  label: string
  icon: IconName
}

interface GuideContent {
  key: GuideKey
  hero: {
    badge: string
    badgeClass: string
    title: string
    desc: string
    primaryAction: string
    secondaryAction: {
      label: string
      to: string
      icon: IconName
    }
    tags: Array<{ label: string; icon: IconName }>
  }
  quickSteps: Array<{ title: string; desc: string }>
  downloadsTitle: string
  downloadsDesc: string
  downloadCards: DownloadCard[]
  showCcSwitch: boolean
  workflowTitle: string
  workflowCards: WorkflowCard[]
  apiBaseHint: string
  checkTitle: string
  checkDesc: string
  checklist: string[]
  faqItems: Array<{ q: string; a: string }>
}

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const codexCliDocsUrl = 'https://developers.openai.com/codex/cli'
const codexAppDocsUrl = 'https://developers.openai.com/codex/app'
const codexCliInstall = 'npm install -g @openai/codex'
const claudeCodeDocsUrl = 'https://docs.anthropic.com/en/docs/claude-code/setup'
const claudeCodeNativeInstall = 'curl -fsSL https://claude.ai/install.sh | bash'
const claudeCodeNpmInstall = 'npm install -g @anthropic-ai/claude-code'

const imageStudioDownloads = [
  {
    label: 'macOS Apple Silicon',
    actionLabel: '下载 DMG',
    href: '/downloads/image-studio/本地生图工作台-0.1.0-arm64.dmg',
    note: '适合 M 系列 Mac。安装后打开设置，把平台生图 Key 写进去即可使用。',
    external: false
  },
  {
    label: 'Windows 便携版',
    actionLabel: '下载 EXE',
    href: '/downloads/image-studio/本地生图工作台-0.1.0-portable.exe',
    note: '适合大多数 Windows 用户。下载后直接运行，再在设置里填写生图 Key。',
    external: false
  }
]

const codexQuickSteps = [
  { title: '下载 Codex 和 CC-Switch', desc: 'CLI 走统一安装；桌面端按 Windows / macOS 选择。' },
  { title: '登录平台并创建 API 密钥', desc: '进入 API 密钥页，创建并妥善保存密钥。' },
  { title: '给密钥确认分组', desc: '分组决定密钥会按 OpenAI、Claude、Gemini 等哪种客户端配置。' },
  { title: '点击“导入到 CCS”', desc: '系统会调用 CC-Switch 的导入协议，把地址和密钥写入本地配置。' },
  { title: '打开 Codex 测试', desc: '确认能看到模型列表或拿到一次正常回复后再正式使用。' }
]

const codexWorkflowCards: WorkflowCard[] = [
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

const codexChecklist = [
  'Codex 已安装；CLI 用户可在终端执行 codex 检查是否可启动。',
  'CC-Switch 已安装并能正常打开，系统协议处理程序已注册。',
  '平台 API 密钥已创建、未泄露，并且绑定了正确分组。',
  '点击“导入到 CCS”后，CC-Switch 中能看到对应 Provider。',
  '第一次测试只发简单问题，确认没有 401、403、404 或模型不可用报错。',
  '如果需要手动配置，优先复制“使用密钥”弹窗里的配置内容。'
]

const codexFaqItems = [
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

const claudeQuickSteps = [
  { title: '安装 Claude Code', desc: '按官方文档安装，命令行用户推荐先确认 Node.js 18+。' },
  { title: '安装并打开 CC-Switch', desc: 'CCS 负责把本平台的接口地址和密钥导入到 Claude Code 配置。' },
  { title: '创建 Claude 分组密钥', desc: '密钥所在分组需要支持 Claude / Anthropic 或 Antigravity Claude。' },
  { title: '点击“导入到 CCS”', desc: 'Anthropic 分组会导入为 Claude 配置，Antigravity 分组会按对应端点写入。' },
  { title: '打开 Claude Code 测试', desc: '在项目目录运行 claude，确认能发起一次正常会话。' }
]

const claudeWorkflowCards: WorkflowCard[] = [
  {
    title: '先装 Claude Code',
    desc: '按官方 setup 文档安装。macOS / Linux 可用原生脚本；已配置 Node.js 的环境也可以用 npm 全局安装。',
    success: '终端执行 claude 能进入 Claude Code。 ',
    icon: 'terminal',
    iconClass: 'bg-slate-900 text-white dark:bg-white dark:text-slate-950'
  },
  {
    title: '创建 Claude 密钥',
    desc: '进入“API 密钥”页面创建新密钥，并选择支持 Claude 的分组。不要把 Codex/OpenAI 分组误用到 Claude Code。',
    success: '密钥行显示 Claude、Anthropic 或 Antigravity Claude 对应分组。',
    icon: 'key',
    iconClass: 'bg-emerald-600 text-white'
  },
  {
    title: '导入到 CCS',
    desc: '先打开 CC-Switch，再点击密钥行的“导入到 CCS”。系统会根据分组平台写入 Claude 端点和 API Key。',
    success: 'CC-Switch 中出现该站点 Provider，app 类型为 Claude。',
    icon: 'upload',
    iconClass: 'bg-blue-600 text-white'
  },
  {
    title: '运行 Claude Code',
    desc: '回到终端进入项目目录，执行 claude 后发送一个简单问题。首次测试不要直接跑大任务。',
    success: 'Claude Code 能正常回复，没有 401、403 或模型不可用提示。',
    icon: 'chat',
    iconClass: 'bg-amber-500 text-white'
  }
]

const claudeChecklist = [
  'Claude Code 已安装，终端执行 claude 有响应。',
  'CC-Switch 已打开，并能接收 ccswitch:// 导入协议。',
  '平台密钥绑定的是 Claude / Anthropic / Antigravity Claude 分组。',
  '导入后 CC-Switch 中 Provider 的 app 类型是 Claude，不是 Codex。',
  '第一次测试只问简单问题，确认没有鉴权或模型路由报错。',
  '如果需要手动配置，优先复制“使用密钥”弹窗中的 Anthropic/Claude 配置。'
]

const claudeFaqItems = [
  {
    q: 'Claude Code 安装后提示找不到 claude 命令怎么办？',
    a: '先重新打开终端，再检查安装目录是否进入 PATH。如果用 npm 安装，请确认 Node.js 版本不低于 18，并检查 npm 全局 bin 路径。'
  },
  {
    q: '导入后 Claude Code 还是走官方接口怎么办？',
    a: '通常是 CC-Switch 没有切到刚导入的 Provider，或导入时选错了分组。请在 CC-Switch 中确认当前 Claude Provider，再重新打开终端测试。'
  },
  {
    q: 'Claude Code 和 Codex 能共用同一个 Key 吗？',
    a: '可以使用同一平台生成的 Key，但分组必须支持对应客户端。Codex 通常走 OpenAI 分组，Claude Code 需要 Claude/Anthropic 兼容分组。'
  },
  {
    q: '出现 401 或 403 怎么处理？',
    a: '先检查 Key 是否完整、状态是否启用、分组是否正确。仍失败时重新创建一个密钥，并用“使用密钥”弹窗里的配置手动核对。'
  }
]

const imageQuickSteps = [
  { title: '先确认使用方式', desc: '网页上可用模型测试里的图片模式；本地软件适合批量、长时间创作。' },
  { title: '准备生图 Key', desc: '在平台创建支持图片生成的密钥，分组需开启图片生成能力。' },
  { title: '网页创意绘图', desc: '进入模型测试，切到图片模式，选择图片模型和分组后输入提示词。' },
  { title: '下载本地生图工作台', desc: '按系统下载 macOS 或 Windows 安装包。' },
  { title: '在设置里填 Key', desc: '打开本地软件设置，把生图 Key 和接口地址写进去后保存。' }
]

const imageWorkflowCards: WorkflowCard[] = [
  {
    title: '路线一：网页图片模式',
    desc: '登录后进入“模型测试”，选择图片模式。这里可以直接试提示词、参考图、尺寸和当前分组价格，适合快速确认 Key 可用。',
    success: '页面能返回图片预览，并在用量记录里看到图片请求。',
    icon: 'sparkles',
    iconClass: 'bg-pink-600 text-white'
  },
  {
    title: '检查图片分组',
    desc: '密钥必须绑定开启图片生成的分组。若下拉里没有图片模型，优先检查分组权限和可用渠道。',
    success: '模型测试页图片模式下能选到图片模型与目标分组。',
    icon: 'shield',
    iconClass: 'bg-blue-600 text-white'
  },
  {
    title: '路线二：下载本地软件',
    desc: '下载本地生图工作台后安装或直接运行。它不需要复杂配置，只需要能拿到本站的生图 Key。',
    success: '本地软件能打开设置页。',
    icon: 'download',
    iconClass: 'bg-slate-900 text-white dark:bg-white dark:text-slate-950'
  },
  {
    title: '填写 Key 后保存',
    desc: '在本地软件设置里填写 API Key 和接口地址。接口地址可用当前站点地址，或以“使用密钥”弹窗给出的配置为准。',
    success: '保存后发起一次简单生图任务，可以正常生成图片。',
    icon: 'cog',
    iconClass: 'bg-emerald-600 text-white'
  }
]

const imageChecklist = [
  '平台密钥已创建，并绑定支持图片生成的分组。',
  '网页路线：模型测试页已切到图片模式，能选择图片模型。',
  '网页路线：首次只用简单提示词测试，不上传过大的参考图。',
  '本地路线：已下载对应系统的本地生图工作台安装包。',
  '本地路线：设置里已填写 API Key 和接口地址，并保存成功。',
  '如果提示模型不可用，先到模型广场或可用渠道页面确认当前图片模型。'
]

const imageFaqItems = [
  {
    q: '网页上的创意绘图在哪里用？',
    a: '当前页面按真实入口引导到“模型测试”的图片模式。登录后进入控制台，打开模型测试，切换到图片模式即可进行提示词生图和参考图编辑。'
  },
  {
    q: '本地生图软件需要配置什么？',
    a: '只需要把平台生成的生图 Key 写进设置，并填写接口地址。接口地址优先使用“使用密钥”弹窗中的 Base URL。'
  },
  {
    q: '为什么选择不到图片模型？',
    a: '通常是密钥分组没有开启图片生成，或当前分组没有可用图片渠道。请先检查分组、模型广场和可用渠道。'
  },
  {
    q: '安装包下载后打不开怎么办？',
    a: 'macOS 需要选择对应 Apple Silicon 包；Windows 便携版可以直接运行。如果系统拦截，请确认文件来自本站下载入口后再按系统提示放行。'
  }
]

const guideTabs: GuideTab[] = [
  { key: 'codex', label: 'Codex', icon: 'terminal' },
  { key: 'claudecode', label: 'Claude Code', icon: 'chat' },
  { key: 'image', label: '生图', icon: 'sparkles' }
]

const guideContents: Record<GuideKey, GuideContent> = {
  codex: {
    key: 'codex',
    hero: {
      badge: '新手必看',
      badgeClass: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/70 dark:bg-emerald-950/50 dark:text-emerald-300',
      title: '小白使用攻略：从新建密钥到 Codex + CC-Switch 正常使用',
      desc: '按顺序完成工具下载、创建 API 密钥、导入到 CC-Switch，再打开 Codex 使用。每一步都配了成功标志和排错入口，适合第一次配置的用户照着做。',
      primaryAction: '先下载工具',
      secondaryAction: { label: '已登录，去新建密钥', to: '/keys', icon: 'key' },
      tags: [
        { label: '密钥不要公开', icon: 'shield' },
        { label: '推荐先装 Codex 桌面端 + CC-Switch', icon: 'checkCircle' }
      ]
    },
    quickSteps: codexQuickSteps,
    downloadsTitle: '下载必备工具',
    downloadsDesc: 'Codex 分为 CLI 和桌面端。桌面端和 CC-Switch 都由本站提供对应系统的安装包，不需要再打开 GitHub 或 Microsoft Store。',
    downloadCards: [
      {
        title: 'Codex CLI',
        kicker: '不按设备拆分',
        desc: '适合会使用终端、需要命令行调用的用户。官方安装方式以 Codex CLI 文档为准。',
        icon: 'terminal',
        iconClass: 'bg-slate-900 text-white dark:bg-white dark:text-slate-950',
        command: codexCliInstall,
        copyKey: 'codex-cli',
        actions: [{ label: '官方安装说明', href: codexCliDocsUrl, external: true }]
      },
      {
        title: 'Codex 桌面端',
        kicker: '按系统选择',
        desc: '完全新手优先选择桌面端。Windows 可直接下载官方 MSIX，不需要打开 Microsoft Store；macOS 下载官方 DMG；Linux 建议使用 CLI。',
        icon: 'download',
        iconClass: 'bg-blue-600 text-white',
        wide: true,
        items: codexDesktopDownloads,
        actions: [{ label: '官方说明', href: codexAppDocsUrl, external: true }]
      }
    ],
    showCcSwitch: true,
    workflowTitle: '从创建密钥到导入 CC-Switch',
    workflowCards: codexWorkflowCards,
    apiBaseHint: '如果需要手动配置客户端，优先使用后台“使用密钥”弹窗给出的内容；接口地址通常是当前站点地址或管理员配置的 API Base URL。',
    checkTitle: '第一次运行前检查',
    checkDesc: '完成导入后，先做一次轻量测试。能看到模型列表或正常回复，再开始正式使用。',
    checklist: codexChecklist,
    faqItems: codexFaqItems
  },
  claudecode: {
    key: 'claudecode',
    hero: {
      badge: 'Claude Code 教程',
      badgeClass: 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/70 dark:bg-amber-950/40 dark:text-amber-200',
      title: 'Claude Code 小白攻略：下载安装后用 CCS 导入本站密钥',
      desc: '这条路线适合想使用 Claude Code 的用户：先安装 Claude Code 和 CC-Switch，再创建支持 Claude 的平台密钥，最后通过“导入到 CCS”完成本地配置。',
      primaryAction: '下载 Claude Code',
      secondaryAction: { label: '去新建 Claude 密钥', to: '/keys', icon: 'key' },
      tags: [
        { label: '官方要求 Node.js 18+', icon: 'infoCircle' },
        { label: 'Claude 分组不要和 Codex 分组混用', icon: 'shield' }
      ]
    },
    quickSteps: claudeQuickSteps,
    downloadsTitle: '下载 Claude Code 和 CC-Switch',
    downloadsDesc: 'Claude Code 官方提供原生安装脚本，也支持 npm 全局安装。导入本站配置仍通过 CC-Switch 完成。',
    downloadCards: [
      {
        title: 'Claude Code 原生安装',
        kicker: '官方推荐入口',
        desc: '适合 macOS / Linux 终端用户。安装后重新打开终端，再执行 claude 检查是否可启动。',
        icon: 'terminal',
        iconClass: 'bg-slate-900 text-white dark:bg-white dark:text-slate-950',
        command: claudeCodeNativeInstall,
        copyKey: 'claude-native',
        actions: [{ label: '官方安装说明', href: claudeCodeDocsUrl, external: true }]
      },
      {
        title: 'Claude Code npm 安装',
        kicker: 'Node.js 18+',
        desc: '如果电脑已经有 Node.js 18 或更高版本，可以用 npm 全局安装。Windows 用户通常更适合走 WSL 或按官方文档的系统说明操作。',
        icon: 'download',
        iconClass: 'bg-amber-600 text-white',
        wide: true,
        command: claudeCodeNpmInstall,
        copyKey: 'claude-npm',
        actions: [{ label: '查看 setup 文档', href: claudeCodeDocsUrl, external: true }]
      }
    ],
    showCcSwitch: true,
    workflowTitle: '从 Claude 密钥到 CCS 导入',
    workflowCards: claudeWorkflowCards,
    apiBaseHint: 'Claude Code 手动配置时，请以“使用密钥”弹窗给出的 Anthropic / Claude 配置为准；Antigravity Claude 通常需要使用带 /antigravity 的端点。',
    checkTitle: '第一次运行前检查',
    checkDesc: '完成导入后，先在一个测试目录里运行 Claude Code。确认配置走的是本站 Provider，再开始真实项目任务。',
    checklist: claudeChecklist,
    faqItems: claudeFaqItems
  },
  image: {
    key: 'image',
    hero: {
      badge: '生图双路线',
      badgeClass: 'border-pink-200 bg-pink-50 text-pink-700 dark:border-pink-900/70 dark:bg-pink-950/40 dark:text-pink-200',
      title: '生图小白攻略：网页创意绘图，或下载本地生图工作台',
      desc: '生图分两种用法：网页上直接用图片模式快速测试和创作；本地软件适合更完整的创作流程，下载安装后只要把生图 Key 写进设置即可。',
      primaryAction: '选择生图方式',
      secondaryAction: { label: '去模型测试', to: '/model-test', icon: 'sparkles' },
      tags: [
        { label: '分组必须支持图片生成', icon: 'shield' },
        { label: '本地软件只需填写生图 Key', icon: 'key' }
      ]
    },
    quickSteps: imageQuickSteps,
    downloadsTitle: '网页入口和本地安装包',
    downloadsDesc: '网页路线不需要额外安装；本地路线按系统下载生图工作台，再在设置中填写 Key 和接口地址。',
    downloadCards: [
      {
        title: '网页创意绘图',
        kicker: '无需安装',
        desc: '登录后进入模型测试，切到图片模式即可浏览可用图片模型、输入提示词、上传参考图并测试生成效果。',
        icon: 'sparkles',
        iconClass: 'bg-pink-600 text-white',
        actions: [{ label: '打开模型测试', href: '/model-test', external: false }]
      },
      {
        title: '本地生图工作台',
        kicker: 'macOS / Windows',
        desc: '适合桌面端创作。下载对应系统安装包，打开设置，填入本站生图 Key 和接口地址即可。',
        icon: 'download',
        iconClass: 'bg-slate-900 text-white dark:bg-white dark:text-slate-950',
        wide: true,
        items: imageStudioDownloads,
        actions: []
      }
    ],
    showCcSwitch: false,
    workflowTitle: '两种生图方式怎么走',
    workflowCards: imageWorkflowCards,
    apiBaseHint: '生图本地软件需要接口地址时，可先使用当前站点地址；如果“使用密钥”弹窗给出了专门的 Base URL，请以弹窗内容为准。',
    checkTitle: '第一次生图前检查',
    checkDesc: '图片请求比普通文本请求更容易暴露分组、模型和尺寸配置问题。首次测试建议用简单提示词和默认尺寸。',
    checklist: imageChecklist,
    faqItems: imageFaqItems
  }
}

const copiedKey = ref('')
const activeGuideKey = ref<GuideKey>('codex')
const isDark = ref(document.documentElement.classList.contains('dark'))

const activeGuide = computed(() => guideContents[activeGuideKey.value])
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
