<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl">
      <div class="border-b border-gray-200 pb-6 dark:border-dark-700">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div class="min-w-0">
            <p class="text-sm font-medium text-primary-600 dark:text-primary-400">API 开发文档</p>
            <h2 class="mt-2 text-3xl font-semibold text-gray-950 dark:text-white">直接通过 HTTP 接入文本、图片与视频</h2>
            <p class="mt-3 max-w-3xl text-sm leading-6 text-gray-600 dark:text-dark-300">
              适合不使用 Codex、Claude Code 或专用客户端的开发者。所有示例都使用标准 HTTP 请求，可在服务端、脚本或自动化平台中调用。
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <router-link to="/keys" class="btn btn-secondary">
              <Icon name="key" size="sm" />
              管理 API Key
            </router-link>
            <router-link to="/models" class="btn btn-primary">
              查看可用模型
            </router-link>
          </div>
        </div>
      </div>

      <nav class="sticky top-16 z-20 -mx-4 overflow-x-auto border-b border-gray-200 bg-gray-50/95 px-4 py-3 backdrop-blur dark:border-dark-700 dark:bg-dark-950/95 lg:hidden" aria-label="开发文档目录">
        <div class="flex min-w-max gap-1">
          <a
            v-for="item in sections"
            :key="item.id"
            :href="`#${item.id}`"
            class="rounded-md px-3 py-1.5 text-sm font-medium"
            :class="activeSection === item.id
              ? 'bg-gray-200 text-gray-950 dark:bg-dark-700 dark:text-white'
              : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white'"
            @click.prevent="scrollToSection(item.id)"
          >
            {{ item.shortLabel }}
          </a>
        </div>
      </nav>

      <div class="grid gap-10 py-8 lg:grid-cols-[13rem_minmax(0,1fr)]">
        <aside class="hidden lg:block">
          <nav class="sticky top-24" aria-label="开发文档目录">
            <p class="px-3 text-xs font-semibold uppercase text-gray-500 dark:text-dark-400">本页目录</p>
            <div class="mt-3 flex flex-col gap-1">
              <a
                v-for="item in sections"
                :key="item.id"
                :href="`#${item.id}`"
                class="rounded-md px-3 py-2 text-sm font-medium"
                :class="activeSection === item.id
                  ? 'bg-gray-200 text-gray-950 dark:bg-dark-700 dark:text-white'
                  : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white'"
                @click.prevent="scrollToSection(item.id)"
              >
                {{ item.label }}
              </a>
            </div>
          </nav>
        </aside>

        <article class="min-w-0 divide-y divide-gray-200 dark:divide-dark-700">
          <section id="quick-start" class="scroll-mt-28 pb-12">
            <DocHeading number="01" title="快速开始" description="先准备接口地址和 API Key，再用最小请求验证鉴权与模型是否可用。" />

            <div class="mt-6 grid gap-4 sm:grid-cols-3">
              <div v-for="item in prerequisites" :key="item.title" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
                <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ item.title }}</p>
                <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ item.description }}</p>
              </div>
            </div>

            <div class="mt-6 rounded-lg border border-amber-200 bg-amber-50 p-4 dark:border-amber-900/60 dark:bg-amber-950/30">
              <div class="flex items-start gap-2">
                <Icon name="infoCircle" size="sm" class="shrink-0 text-amber-700 dark:text-amber-300" />
                <p class="text-sm leading-6 text-amber-900 dark:text-amber-100">
                  API Key 等同账号凭据，只应保存在服务端环境变量或密钥管理服务中。不要把 Key 写进公开网页、前端包、截图或日志。
                </p>
              </div>
            </div>

            <CodeSnippet class="mt-6" label="Shell 环境变量与模型检查" :code="quickStartCode" />
          </section>

          <section id="text-api" class="scroll-mt-28 py-12">
            <DocHeading number="02" title="文本对话" description="OpenAI 兼容客户端可直接调用 Chat Completions；模型名以模型广场或密钥分组实际可用列表为准。" />
            <CodeSnippet class="mt-6" label="POST /v1/chat/completions" :code="chatCode" />
            <div class="mt-5 overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
              <table class="w-full min-w-[36rem] text-left text-sm">
                <thead class="bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-dark-200">
                  <tr><th class="px-4 py-3 font-semibold">字段</th><th class="px-4 py-3 font-semibold">说明</th></tr>
                </thead>
                <tbody class="divide-y divide-gray-200 bg-white text-gray-600 dark:divide-dark-700 dark:bg-dark-900 dark:text-dark-300">
                  <tr><td class="px-4 py-3 font-mono text-gray-900 dark:text-white">model</td><td class="px-4 py-3">必填。必须是当前 Key 所属分组可调用的模型。</td></tr>
                  <tr><td class="px-4 py-3 font-mono text-gray-900 dark:text-white">messages</td><td class="px-4 py-3">必填。按 role 与 content 传入对话消息。</td></tr>
                  <tr><td class="px-4 py-3 font-mono text-gray-900 dark:text-white">stream</td><td class="px-4 py-3">可选。设为 true 时使用 SSE 流式接收。</td></tr>
                </tbody>
              </table>
            </div>
          </section>

          <section id="image-api" class="scroll-mt-28 py-12">
            <DocHeading number="03" title="图片生成" description="图片接口兼容 OpenAI Images API。重点先选择返回方式：内联 Base64，或本站托管的临时 URL。" />

            <div class="mt-6 grid gap-4 md:grid-cols-2">
              <div class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-gray-900 dark:text-white">Base64 返回</p>
                    <p class="mt-1 text-xs font-mono text-primary-600 dark:text-primary-400">response_format: b64_json</p>
                  </div>
                  <span class="rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-dark-300">适合落盘</span>
                </div>
                <p class="mt-4 text-sm leading-6 text-gray-600 dark:text-dark-300">响应字段是 <code class="font-mono text-gray-900 dark:text-white">data[].b64_json</code>，不含 Data URI 前缀。客户端需要 Base64 解码后保存，或自行拼接 MIME 前缀。</p>
              </div>
              <div class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-gray-900 dark:text-white">URL 返回</p>
                    <p class="mt-1 text-xs font-mono text-primary-600 dark:text-primary-400">response_format: url</p>
                  </div>
                  <span class="rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-dark-300">适合展示</span>
                </div>
                <p class="mt-4 text-sm leading-6 text-gray-600 dark:text-dark-300">响应字段是 <code class="font-mono text-gray-900 dark:text-white">data[].url</code>。地址无需 API Key 即可读取，默认 24 小时后失效，应及时下载或转存。</p>
              </div>
            </div>

            <h3 class="mt-9 text-xl font-semibold text-gray-950 dark:text-white">文生图请求</h3>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">请求中的 <code class="font-mono text-gray-900 dark:text-white">response_format</code> 优先级最高；未传时使用分组默认值，分组未配置时回退为 <code class="font-mono text-gray-900 dark:text-white">b64_json</code>。</p>
            <div class="mt-5 grid gap-5 xl:grid-cols-2">
              <CodeSnippet label="返回 URL" :code="imageUrlCode" />
              <CodeSnippet label="返回 Base64" :code="imageBase64Code" />
            </div>

            <h3 class="mt-9 text-xl font-semibold text-gray-950 dark:text-white">解析 Base64 响应</h3>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">下面示例会直接把响应中的 Base64 写成图片文件。扩展名应按实际返回格式调整。</p>
            <CodeSnippet class="mt-5" label="Python" :code="pythonBase64Code" />

            <h3 class="mt-9 text-xl font-semibold text-gray-950 dark:text-white">图片编辑与参考图</h3>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">本地文件使用 multipart 上传；公网图片可通过 JSON 的 <code class="font-mono text-gray-900 dark:text-white">images[].image_url</code> 提交。远程地址必须是服务器可访问的公网 HTTP/HTTPS 图片。</p>
            <div class="mt-5 grid gap-5 xl:grid-cols-2">
              <CodeSnippet label="multipart 本地图片" :code="imageEditCode" />
              <CodeSnippet label="JSON 公网图片" :code="remoteImageEditCode" />
            </div>

            <div class="mt-6 overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
              <table class="w-full min-w-[42rem] text-left text-sm">
                <thead class="bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-dark-200"><tr><th class="px-4 py-3 font-semibold">字段</th><th class="px-4 py-3 font-semibold">是否必填</th><th class="px-4 py-3 font-semibold">说明</th></tr></thead>
                <tbody class="divide-y divide-gray-200 bg-white text-gray-600 dark:divide-dark-700 dark:bg-dark-900 dark:text-dark-300">
                  <tr v-for="row in imageFields" :key="row.field"><td class="px-4 py-3 font-mono text-gray-900 dark:text-white">{{ row.field }}</td><td class="px-4 py-3">{{ row.required }}</td><td class="px-4 py-3">{{ row.description }}</td></tr>
                </tbody>
              </table>
            </div>

            <div class="mt-6 rounded-lg border border-blue-200 bg-blue-50 p-4 dark:border-blue-900/60 dark:bg-blue-950/30">
              <p class="text-sm font-semibold text-blue-900 dark:text-blue-100">流式图片的返回差异</p>
              <p class="mt-2 text-sm leading-6 text-blue-800 dark:text-blue-200">使用 SSE 时，URL 模式的中间 <code class="font-mono">partial_image</code> 事件仍可能携带 <code class="font-mono">b64_json</code>，只有最终完成事件返回 URL。Base64 模式的最终事件继续返回 <code class="font-mono">b64_json</code>。</p>
            </div>
          </section>

          <section id="video-api" class="scroll-mt-28 py-12">
            <DocHeading number="04" title="视频生成" description="视频生成是异步任务：先创建任务，轮询完成状态，再通过本站内容接口下载 MP4。" />

            <ol class="mt-6 grid gap-4 sm:grid-cols-3" role="list">
              <li v-for="(step, index) in videoSteps" :key="step.title" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
                <p class="text-xs font-semibold tabular-nums text-primary-600 dark:text-primary-400">步骤 {{ index + 1 }}</p>
                <p class="mt-2 text-sm font-semibold text-gray-900 dark:text-white">{{ step.title }}</p>
                <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ step.description }}</p>
              </li>
            </ol>

            <div class="mt-6 overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
              <table class="w-full min-w-[44rem] text-left text-sm">
                <thead class="bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-dark-200"><tr><th class="px-4 py-3 font-semibold">模型</th><th class="px-4 py-3 font-semibold">图片输入</th><th class="px-4 py-3 font-semibold">限制</th></tr></thead>
                <tbody class="divide-y divide-gray-200 bg-white text-gray-600 dark:divide-dark-700 dark:bg-dark-900 dark:text-dark-300">
                  <tr><td class="px-4 py-3 font-mono text-gray-900 dark:text-white">grok-imagine-video</td><td class="px-4 py-3 font-mono">reference_images[].url</td><td class="px-4 py-3">最多 4 张参考图，用于人物、物品或风格参考，不固定为首帧。</td></tr>
                  <tr><td class="px-4 py-3 font-mono text-gray-900 dark:text-white">grok-imagine-video-1.5</td><td class="px-4 py-3 font-mono">image.url</td><td class="px-4 py-3">最多 1 张起始图。未提供起始图时按标准视频模型路由。</td></tr>
                </tbody>
              </table>
            </div>

            <div class="mt-6 grid gap-5 xl:grid-cols-2">
              <CodeSnippet label="标准模型：参考图引导" :code="standardVideoCode" />
              <CodeSnippet label="1.5 模型：起始图生成" :code="startingImageVideoCode" />
            </div>

            <div class="mt-6 rounded-lg border border-amber-200 bg-amber-50 p-4 dark:border-amber-900/60 dark:bg-amber-950/30">
              <p class="text-sm leading-6 text-amber-900 dark:text-amber-100">不要在同一请求中同时传 <code class="font-mono">image</code> 和 <code class="font-mono">reference_images</code>。内联 Data URL 图片解码后不得超过 1 MB；过大请求会返回 413，应先压缩图片。</p>
            </div>

            <h3 class="mt-9 text-xl font-semibold text-gray-950 dark:text-white">轮询任务状态</h3>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">创建接口通常返回 <code class="font-mono text-gray-900 dark:text-white">request_id</code>。建议每 2 秒查询一次，最长等待时间由客户端自行控制。</p>
            <CodeSnippet class="mt-5" label="GET /v1/videos/{request_id}" :code="videoPollingCode" />

            <h3 class="mt-9 text-xl font-semibold text-gray-950 dark:text-white">下载视频内容</h3>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">任务完成后，通过本站 content 接口携带同一个 API Key 获取视频。不要依赖状态响应中的上游 CDN 地址。</p>
            <CodeSnippet class="mt-5" label="GET /v1/videos/{request_id}/content" :code="videoDownloadCode" />
          </section>

          <section id="errors" class="scroll-mt-28 py-12">
            <DocHeading number="05" title="错误处理" description="所有请求都应记录 HTTP 状态码、响应中的 error.message，以及响应头中的请求标识，便于定位问题。" />
            <div class="mt-6 overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
              <table class="w-full min-w-[42rem] text-left text-sm">
                <thead class="bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-dark-200"><tr><th class="px-4 py-3 font-semibold">状态码</th><th class="px-4 py-3 font-semibold">常见原因</th><th class="px-4 py-3 font-semibold">处理建议</th></tr></thead>
                <tbody class="divide-y divide-gray-200 bg-white text-gray-600 dark:divide-dark-700 dark:bg-dark-900 dark:text-dark-300">
                  <tr v-for="row in errorRows" :key="row.status"><td class="px-4 py-3 font-mono font-semibold text-gray-900 dark:text-white">{{ row.status }}</td><td class="px-4 py-3">{{ row.reason }}</td><td class="px-4 py-3">{{ row.action }}</td></tr>
                </tbody>
              </table>
            </div>
          </section>

          <section id="checklist" class="scroll-mt-28 pt-12">
            <DocHeading number="06" title="上线前检查" description="正式接入前，用最小请求逐项确认鉴权、模型、返回格式和文件保存策略。" />
            <ul class="mt-6 grid gap-3 sm:grid-cols-2" role="list">
              <li v-for="item in checklist" :key="item" class="flex items-start gap-2 rounded-lg border border-gray-200 bg-white p-4 text-sm leading-6 text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200">
                <Icon name="check" size="sm" class="shrink-0 text-emerald-600 dark:text-emerald-400" />
                <span>{{ item }}</span>
              </li>
            </ul>
          </section>
        </article>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, onMounted, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import CodeSnippet from '@/components/docs/CodeSnippet.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'

const DocHeading = defineComponent({
  props: {
    number: { type: String, required: true },
    title: { type: String, required: true },
    description: { type: String, required: true }
  },
  setup(props) {
    return () => h('div', { class: 'max-w-3xl' }, [
      h('p', { class: 'text-xs font-semibold tabular-nums text-primary-600 dark:text-primary-400' }, props.number),
      h('h2', { class: 'mt-2 text-2xl font-semibold text-gray-950 dark:text-white' }, props.title),
      h('p', { class: 'mt-3 text-sm leading-6 text-gray-600 dark:text-dark-300' }, props.description)
    ])
  }
})

const appStore = useAppStore()
const activeSection = ref('quick-start')
let observer: IntersectionObserver | null = null

const sections = [
  { id: 'quick-start', label: '快速开始', shortLabel: '开始' },
  { id: 'text-api', label: '文本对话', shortLabel: '文本' },
  { id: 'image-api', label: '图片生成', shortLabel: '图片' },
  { id: 'video-api', label: '视频生成', shortLabel: '视频' },
  { id: 'errors', label: '错误处理', shortLabel: '错误' },
  { id: 'checklist', label: '上线检查', shortLabel: '检查' }
]

const prerequisites = [
  { title: '接口地址', description: '优先使用“使用密钥”弹窗提供的 Base URL；未单独配置时通常就是当前站点地址。' },
  { title: 'API Key', description: '在 API 密钥页面创建，并绑定支持目标模型与媒体能力的分组。' },
  { title: '模型名称', description: '从模型广场或模型测试台确认，不要根据示例名称猜测当前分组一定可用。' }
]

const imageFields = [
  { field: 'model', required: '是', description: '图片模型名称，例如 gpt-image-2。' },
  { field: 'prompt', required: '文生图必填', description: '图片描述或编辑指令。' },
  { field: 'response_format', required: '否', description: 'b64_json 或 url；显式参数优先于分组默认值。' },
  { field: 'n', required: '否', description: '生成数量，默认 1；实际范围由模型决定。' },
  { field: 'size', required: '否', description: '图片尺寸，支持范围以目标模型为准。' },
  { field: 'stream', required: '否', description: '设为 true 时通过 SSE 接收图片事件。' }
]

const videoSteps = [
  { title: '创建任务', description: 'POST /v1/videos/generations，保存返回的 request_id。' },
  { title: '轮询状态', description: 'GET /v1/videos/{request_id}，直到 completed、succeeded、success 或 done。' },
  { title: '读取内容', description: 'GET /v1/videos/{request_id}/content，携带原 API Key 下载 MP4。' }
]

const errorRows = [
  { status: '400', reason: '字段缺失、格式错误、模型与图片输入方式不匹配。', action: '核对请求体、Content-Type 与字段名。' },
  { status: '401', reason: 'API Key 缺失、无效或已被撤销。', action: '重新创建 Key，并确认 Authorization 使用 Bearer。' },
  { status: '403', reason: '分组未开放图片/视频能力，或余额与权限检查未通过。', action: '检查 Key 绑定分组、余额和目标能力。' },
  { status: '404', reason: '模型/入口不可用，或临时图片 URL 已过期。', action: '检查模型广场；URL 图片需在 24 小时内转存。' },
  { status: '409', reason: '视频任务尚未完成，content 暂不可读取。', action: '继续轮询状态，不要立即重复创建任务。' },
  { status: '413', reason: '视频内联参考图超过 1 MB 等请求体限制。', action: '压缩图片后重试。' },
  { status: '429', reason: '用户、Key、账号并发或上游频率受限。', action: '指数退避并限制客户端并发，尊重 Retry-After。' },
  { status: '5xx', reason: '上游异常、无可调度账号或网关暂时不可用。', action: '保留请求标识和响应体，稍后重试或联系管理员。' }
]

const checklist = [
  'API Key 只保存在服务端，不会随网页代码或客户端安装包公开。',
  '先用最小文本请求确认 Base URL、鉴权和目标模型可用。',
  '图片客户端能同时识别 data[].b64_json 与 data[].url。',
  'URL 图片会在 24 小时内下载或转存到自己的长期存储。',
  '视频创建、轮询、失败状态和 content 下载都设置了超时。',
  '对 429 和临时 5xx 使用带抖动的指数退避，不进行无限重试。'
]

const apiBaseUrl = computed(() => {
  const configured = appStore.cachedPublicSettings?.api_base_url?.trim()
  const fallback = typeof window === 'undefined' ? 'https://api.example.com' : window.location.origin
  return (configured || fallback).replace(/\/$/, '')
})

const quickStartCode = computed(() => `export BASE_URL="${apiBaseUrl.value}"
export API_KEY="sk-your-api-key"

curl "$BASE_URL/v1/models" \\
  -H "Authorization: Bearer $API_KEY"`)

const chatCode = computed(() => `curl "$BASE_URL/v1/chat/completions" \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "YOUR_TEXT_MODEL",
    "messages": [
      {"role": "user", "content": "用三句话介绍这个 API"}
    ],
    "stream": false
  }'`)

const imageUrlCode = computed(() => `curl "$BASE_URL/v1/images/generations" \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-image-2",
    "prompt": "一座雨夜中的未来城市，电影感光线",
    "size": "1536x1024",
    "n": 1,
    "response_format": "url"
  }'`)

const imageBase64Code = computed(() => `curl "$BASE_URL/v1/images/generations" \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-image-2",
    "prompt": "白色背景上的产品摄影，柔和棚拍光",
    "n": 1,
    "response_format": "b64_json"
  }'`)

const pythonBase64Code = computed(() => `import base64
import os
import requests

response = requests.post(
    "${apiBaseUrl.value}/v1/images/generations",
    headers={"Authorization": f"Bearer {os.environ['API_KEY']}"},
    json={
        "model": "gpt-image-2",
        "prompt": "极简风格的玻璃水杯产品图",
        "response_format": "b64_json",
    },
    timeout=300,
)
response.raise_for_status()

image_base64 = response.json()["data"][0]["b64_json"]
with open("generated-image.png", "wb") as image_file:
    image_file.write(base64.b64decode(image_base64))`)

const imageEditCode = computed(() => `curl "$BASE_URL/v1/images/edits" \\
  -H "Authorization: Bearer $API_KEY" \\
  -F "model=gpt-image-2" \\
  -F "prompt=保留主体，把背景改成日落海边" \\
  -F "image=@./source.png" \\
  -F "n=1" \\
  -F "response_format=url"`)

const remoteImageEditCode = computed(() => `curl "$BASE_URL/v1/images/edits" \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-image-2",
    "prompt": "将参考图改成水彩插画",
    "images": [
      {"image_url": "https://example.com/source.png"}
    ],
    "response_format": "url"
  }'`)

const standardVideoCode = computed(() => `curl "$BASE_URL/v1/videos/generations" \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "grok-imagine-video",
    "prompt": "围绕产品缓慢运镜，保持主体细节稳定",
    "resolution": "720p",
    "duration": 10,
    "reference_images": [
      {"url": "https://example.com/product.jpg"}
    ]
  }'`)

const startingImageVideoCode = computed(() => `curl "$BASE_URL/v1/videos/generations" \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "grok-imagine-video-1.5",
    "prompt": "镜头向前推进，云层缓慢移动",
    "resolution": "1080p",
    "duration": 8,
    "image": {
      "url": "data:image/jpeg;base64,YOUR_COMPRESSED_IMAGE_BASE64"
    }
  }'`)

const videoPollingCode = computed(() => `export REQUEST_ID="video-request-id"

curl "$BASE_URL/v1/videos/$REQUEST_ID" \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Accept: application/json"

# 完成状态通常为 completed / succeeded / success / done
# 失败状态通常为 failed / error / cancelled / canceled`)

const videoDownloadCode = computed(() => `curl "$BASE_URL/v1/videos/$REQUEST_ID/content" \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Accept: video/mp4,video/*" \\
  --output generated-video.mp4`)

function scrollToSection(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  activeSection.value = id
}

onMounted(() => {
  if (!appStore.publicSettingsLoaded) appStore.fetchPublicSettings()
  observer = new IntersectionObserver((entries) => {
    const visible = entries
      .filter(entry => entry.isIntersecting)
      .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0]
    if (visible?.target.id) activeSection.value = visible.target.id
  }, { rootMargin: '-96px 0px -65% 0px', threshold: [0, 0.1] })
  sections.forEach(item => {
    const element = document.getElementById(item.id)
    if (element) observer?.observe(element)
  })
})

onBeforeUnmount(() => observer?.disconnect())
</script>
