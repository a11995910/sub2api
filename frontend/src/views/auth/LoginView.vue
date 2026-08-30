<template>
  <AuthLayout variant="premium">
    <div data-testid="premium-login" class="premium-login">
      <div class="login-heading">
        <h2>
          {{ t('auth.welcomeBack') }}
        </h2>
        <p>
          {{ t('auth.signInToAccount') }}
        </p>
      </div>

      <Transition name="fade">
        <div v-if="errorMessage" data-testid="login-error" class="login-alert" role="alert">
          <Icon name="exclamationCircle" size="sm" aria-hidden="true" />
          <p>{{ errorMessage }}</p>
        </div>
      </Transition>

      <form class="login-form" novalidate @submit.prevent="handleLogin">
        <div class="login-field">
          <label for="email" class="login-label">
            {{ t('auth.emailLabel') }}
          </label>
          <div class="login-control">
            <Icon name="mail" size="sm" class="login-control__icon" aria-hidden="true" />
            <input
              id="email"
              v-model="formData.email"
              name="email"
              type="email"
              required
              autofocus
              autocomplete="email"
              :disabled="authActionDisabled"
              :aria-invalid="Boolean(errors.email)"
              :aria-describedby="errors.email ? 'email-error' : undefined"
              class="input login-input"
              :class="{ 'input-error': errors.email }"
              :placeholder="t('auth.emailPlaceholder')"
            />
          </div>
          <p v-if="errors.email" id="email-error" class="login-field__error" role="alert">
            {{ errors.email }}
          </p>
        </div>

        <div class="login-field">
          <div class="login-label-row">
            <label for="password" class="login-label">
              {{ t('auth.passwordLabel') }}
            </label>
            <router-link
              v-if="passwordResetEnabled && !backendModeEnabled"
              to="/forgot-password"
              class="login-inline-link"
            >
              {{ t('auth.forgotPassword') }}
            </router-link>
          </div>
          <div class="login-control">
            <Icon name="lock" size="sm" class="login-control__icon" aria-hidden="true" />
            <input
              id="password"
              v-model="formData.password"
              name="password"
              :type="showPassword ? 'text' : 'password'"
              required
              autocomplete="current-password"
              :disabled="authActionDisabled"
              :aria-invalid="Boolean(errors.password)"
              :aria-describedby="errors.password ? 'password-error' : undefined"
              class="input login-input login-input--password"
              :class="{ 'input-error': errors.password }"
              :placeholder="t('auth.passwordPlaceholder')"
            />
            <button
              type="button"
              :disabled="authActionDisabled"
              class="login-password-toggle"
              :title="showPassword ? t('auth.hidePassword') : t('auth.showPassword')"
              :aria-label="showPassword ? t('auth.hidePassword') : t('auth.showPassword')"
              :aria-pressed="showPassword"
              data-testid="password-visibility"
              @click="showPassword = !showPassword"
            >
              <Icon v-if="showPassword" name="eyeOff" size="sm" aria-hidden="true" />
              <Icon v-else name="eye" size="sm" aria-hidden="true" />
            </button>
          </div>
          <p
            v-if="errors.password"
            id="password-error"
            class="login-field__error"
            role="alert"
          >
            {{ errors.password }}
          </p>
        </div>

        <div v-if="captchaEnabled" class="login-captcha">
          <TurnstileWidget
            ref="turnstileRef"
            :turnstile-enabled="turnstileEnabled"
            :turnstile-site-key="turnstileSiteKey"
            :tencent-enabled="tencentCaptchaEnabled"
            :tencent-app-id="tencentCaptchaAppId"
            :tencent-region="tencentCaptchaRegion"
            :aliyun-enabled="aliyunCaptchaEnabled"
            :aliyun-scene-id="aliyunCaptchaSceneId"
            :aliyun-prefix="aliyunCaptchaPrefix"
            :aliyun-region="aliyunCaptchaRegion"
            @verify="onTurnstileVerify"
            @expire="onTurnstileExpire"
            @error="onTurnstileError"
          />
          <p v-if="errors.turnstile" class="login-field__error" role="alert">
            {{ errors.turnstile }}
          </p>
        </div>

        <button
          type="submit"
          :disabled="authActionDisabled || (turnstileEnabled && !turnstileToken)"
          class="btn login-submit w-full"
          data-testid="password-login"
        >
          <span v-if="isLoading" class="login-spinner" aria-hidden="true"></span>
          <span>{{ isLoading ? t('auth.signingIn') : t('auth.signIn') }}</span>
          <Icon v-if="!isLoading" name="arrowRight" size="sm" aria-hidden="true" />
        </button>

        <LoginAgreementPrompt
          v-if="loginAgreementEnabled"
          :accepted="agreementAccepted"
          :documents="loginAgreementDocuments"
          :mode="loginAgreementMode"
          :updated-at="loginAgreementUpdatedAt"
          :visible="showAgreementModal"
          @accept="acceptLoginAgreement"
          @reject="rejectLoginAgreement"
          @open="showAgreementModal = true"
        />

        <div v-if="showPasskeyLogin || showOAuthLogin" class="login-alternatives">
          <div class="login-divider">
            <span aria-hidden="true"></span>
            <p>
              {{ t('auth.oauthOrContinue') }}
            </p>
            <span aria-hidden="true"></span>
          </div>

          <button
            v-if="showPasskeyLogin"
            type="button"
            class="btn btn-secondary login-secondary w-full"
            :disabled="authActionDisabled"
            data-testid="passkey-login"
            @click="handlePasskeyLogin"
          >
            <Icon name="key" size="sm" aria-hidden="true" />
            {{ passkeyLoading ? t('auth.passkeySigningIn') : t('auth.passkeySignIn') }}
          </button>

          <EmailOAuthButtons
            :disabled="authActionDisabled"
            :github-enabled="githubOAuthEnabled"
            :google-enabled="googleOAuthEnabled"
            :show-divider="false"
            @start="handleOAuthStart"
          />

          <LinuxDoOAuthSection
            v-if="linuxdoOAuthEnabled"
            :disabled="authActionDisabled"
            :show-divider="false"
            @start="handleOAuthStart"
          />
          <DingTalkOAuthSection
            v-if="dingtalkOAuthEnabled"
            :disabled="authActionDisabled"
            :show-divider="false"
            @start="handleOAuthStart"
          />
          <WechatOAuthSection
            v-if="wechatOAuthEnabled"
            :disabled="authActionDisabled"
            :show-divider="false"
            @start="handleOAuthStart"
          />
          <OidcOAuthSection
            v-if="oidcOAuthEnabled"
            :disabled="authActionDisabled"
            :provider-name="oidcOAuthProviderName"
            :show-divider="false"
            @start="handleOAuthStart"
          />
        </div>
      </form>
    </div>

    <template v-if="!backendModeEnabled" #footer>
      <p class="login-registration">
        {{ t('auth.dontHaveAccount') }}
        <router-link to="/register">
          {{ t('auth.signUp') }}
        </router-link>
      </p>
    </template>
  </AuthLayout>

  <!-- 2FA Modal -->
  <TotpLoginModal
    v-if="show2FAModal"
    ref="totpModalRef"
    :temp-token="totpTempToken"
    :user-email-masked="totpUserEmailMasked"
    @verify="handle2FAVerify"
    @cancel="handle2FACancel"
  />
</template>

<script setup lang="ts">
import { computed, ref, reactive, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import LinuxDoOAuthSection from '@/components/auth/LinuxDoOAuthSection.vue'
import DingTalkOAuthSection from '@/components/auth/DingTalkOAuthSection.vue'
import OidcOAuthSection from '@/components/auth/OidcOAuthSection.vue'
import WechatOAuthSection from '@/components/auth/WechatOAuthSection.vue'
import EmailOAuthButtons from '@/components/auth/EmailOAuthButtons.vue'
import LoginAgreementPrompt from '@/components/auth/LoginAgreementPrompt.vue'
import TotpLoginModal from '@/components/auth/TotpLoginModal.vue'
import Icon from '@/components/icons/Icon.vue'
import TurnstileWidget from '@/components/CaptchaChallenge.vue'
import { useAuthStore, useAppStore } from '@/stores'
import {
  buildOAuthLoginStartURL,
  getPublicSettings,
  isTotp2FARequired,
  isWeChatWebOAuthEnabled,
  startOAuthLogin,
  type OAuthLoginStart
} from '@/api/auth'
import type {
  ActionCaptchaRequestProof,
  LoginAgreementDocument,
  TotpLoginResponse
} from '@/types'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { clearAllAffiliateReferralCodes } from '@/utils/oauthAffiliate'

const { t } = useI18n()
const LOGIN_AGREEMENT_STORAGE_KEY = 'sub2api_login_agreement_consent'

// ==================== Router & Stores ====================

const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()

// ==================== State ====================

const isLoading = ref<boolean>(false)
const passkeyLoading = ref<boolean>(false)
const errorMessage = ref<string>('')
const showPassword = ref<boolean>(false)
const publicSettingsLoaded = ref<boolean>(false)

// Public settings
const turnstileEnabled = ref<boolean>(false)
const turnstileSiteKey = ref<string>('')
const tencentCaptchaEnabled = ref<boolean>(false)
const tencentCaptchaAppId = ref<string>('')
const tencentCaptchaRegion = ref<string>('cn')
const aliyunCaptchaEnabled = ref<boolean>(false)
const aliyunCaptchaSceneId = ref<string>('')
const aliyunCaptchaPrefix = ref<string>('')
const aliyunCaptchaRegion = ref<string>('cn')
const linuxdoOAuthEnabled = ref<boolean>(false)
const dingtalkOAuthEnabled = ref<boolean>(false)
const wechatOAuthEnabled = ref<boolean>(false)
const backendModeEnabled = ref<boolean>(false)
const oidcOAuthEnabled = ref<boolean>(false)
const oidcOAuthProviderName = ref<string>('OIDC')
const githubOAuthEnabled = ref<boolean>(false)
const googleOAuthEnabled = ref<boolean>(false)
const passwordResetEnabled = ref<boolean>(false)
const passkeyEnabled = ref<boolean>(false)
const loginAgreementEnabled = ref<boolean>(false)
const loginAgreementMode = ref<'modal' | 'checkbox' | string>('modal')
const loginAgreementUpdatedAt = ref<string>('')
const loginAgreementRevision = ref<string>('')
const loginAgreementDocuments = ref<LoginAgreementDocument[]>([])
const agreementAccepted = ref<boolean>(false)
const showAgreementModal = ref<boolean>(false)

// Turnstile
const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const turnstileToken = ref<string>('')
const tencentCaptchaRandstr = ref<string>('')
const aliyunCaptchaReady = computed(
  () =>
    aliyunCaptchaEnabled.value &&
    Boolean(aliyunCaptchaSceneId.value) &&
    Boolean(aliyunCaptchaPrefix.value)
)
// 动作触发式验证码（腾讯/阿里云）：提交、OAuth 启动、passkey 时弹窗验证
const actionCaptchaEnabled = computed(
  () =>
    (tencentCaptchaEnabled.value && Boolean(tencentCaptchaAppId.value)) ||
    aliyunCaptchaReady.value
)
const captchaEnabled = computed(
  () =>
    (turnstileEnabled.value && Boolean(turnstileSiteKey.value)) || actionCaptchaEnabled.value
)

// 2FA state
const show2FAModal = ref<boolean>(false)
const totpTempToken = ref<string>('')
const totpUserEmailMasked = ref<string>('')
const totpModalRef = ref<InstanceType<typeof TotpLoginModal> | null>(null)

const formData = reactive({
  email: '',
  password: ''
})

const errors = reactive({
  email: '',
  password: '',
  turnstile: ''
})

const validationToastMessage = computed(
  () => errors.email || errors.password || errors.turnstile || ''
)

const agreementGateActive = computed(
  () => loginAgreementEnabled.value && !agreementAccepted.value
)

const authActionDisabled = computed(
  () => isLoading.value || passkeyLoading.value || !publicSettingsLoaded.value || agreementGateActive.value
)

const showPasskeyLogin = computed(
  () => passkeyEnabled.value && typeof window.PublicKeyCredential !== 'undefined'
)

const showOAuthLogin = computed(
  () =>
    !backendModeEnabled.value &&
    (linuxdoOAuthEnabled.value ||
      dingtalkOAuthEnabled.value ||
      wechatOAuthEnabled.value ||
      oidcOAuthEnabled.value ||
      githubOAuthEnabled.value ||
      googleOAuthEnabled.value)
)

watch(validationToastMessage, (value, previousValue) => {
  if (value && value !== previousValue) {
    appStore.showError(value)
  }
})

// ==================== Lifecycle ====================

onMounted(async () => {
  const expiredFlag = sessionStorage.getItem('auth_expired')
  if (expiredFlag) {
    sessionStorage.removeItem('auth_expired')
    const message = t('auth.reloginRequired')
    errorMessage.value = message
    appStore.showWarning(message)
  }

  try {
    const settings = await getPublicSettings()
    turnstileEnabled.value = settings.turnstile_enabled
    turnstileSiteKey.value = settings.turnstile_site_key || ''
    tencentCaptchaEnabled.value = settings.tencent_captcha_enabled === true
    tencentCaptchaAppId.value = settings.tencent_captcha_app_id || ''
    tencentCaptchaRegion.value = settings.tencent_captcha_region || 'cn'
    aliyunCaptchaEnabled.value = settings.aliyun_captcha_enabled === true
    aliyunCaptchaSceneId.value = settings.aliyun_captcha_scene_id || ''
    aliyunCaptchaPrefix.value = settings.aliyun_captcha_prefix || ''
    aliyunCaptchaRegion.value = settings.aliyun_captcha_region || 'cn'
    linuxdoOAuthEnabled.value = settings.linuxdo_oauth_enabled
    dingtalkOAuthEnabled.value = settings.dingtalk_oauth_enabled ?? false
    wechatOAuthEnabled.value = isWeChatWebOAuthEnabled(settings)
    backendModeEnabled.value = settings.backend_mode_enabled
    oidcOAuthEnabled.value = settings.oidc_oauth_enabled
    oidcOAuthProviderName.value = settings.oidc_oauth_provider_name || 'OIDC'
    githubOAuthEnabled.value = settings.github_oauth_enabled
    googleOAuthEnabled.value = settings.google_oauth_enabled
    backendModeEnabled.value = settings.backend_mode_enabled
    passwordResetEnabled.value = settings.password_reset_enabled
    passkeyEnabled.value = settings.passkey_enabled === true
    applyLoginAgreementSettings(settings)
  } catch (error) {
    console.error('Failed to load public settings:', error)
    loginAgreementEnabled.value = false
    agreementAccepted.value = true
  } finally {
    publicSettingsLoaded.value = true
  }
})

// ==================== Login Agreement ====================

function applyLoginAgreementSettings(settings: {
  login_agreement_enabled?: boolean
  login_agreement_mode?: string
  login_agreement_updated_at?: string
  login_agreement_revision?: string
  login_agreement_documents?: LoginAgreementDocument[]
}): void {
  const documents = Array.isArray(settings.login_agreement_documents)
    ? settings.login_agreement_documents.filter((doc) => doc.title?.trim())
    : []
  loginAgreementDocuments.value = documents
  loginAgreementEnabled.value = settings.login_agreement_enabled === true && documents.length > 0
  loginAgreementMode.value = settings.login_agreement_mode === 'checkbox' ? 'checkbox' : 'modal'
  loginAgreementUpdatedAt.value = settings.login_agreement_updated_at || ''
  loginAgreementRevision.value =
    settings.login_agreement_revision ||
    `${loginAgreementUpdatedAt.value}:${documents.map((doc) => `${doc.id}:${doc.title}`).join('|')}`

  agreementAccepted.value = !loginAgreementEnabled.value || hasAcceptedLoginAgreement(loginAgreementRevision.value)
  showAgreementModal.value =
    loginAgreementEnabled.value && !agreementAccepted.value && loginAgreementMode.value !== 'checkbox'
}

function hasAcceptedLoginAgreement(revision: string): boolean {
  if (!revision) {
    return false
  }
  try {
    const raw = localStorage.getItem(LOGIN_AGREEMENT_STORAGE_KEY)
    if (!raw) {
      return false
    }
    const parsed = JSON.parse(raw) as { revision?: string }
    return parsed.revision === revision
  } catch {
    return false
  }
}

function acceptLoginAgreement(): void {
  if (loginAgreementRevision.value) {
    localStorage.setItem(
      LOGIN_AGREEMENT_STORAGE_KEY,
      JSON.stringify({
        revision: loginAgreementRevision.value,
        accepted_at: new Date().toISOString()
      })
    )
  }
  agreementAccepted.value = true
  showAgreementModal.value = false
}

function rejectLoginAgreement(): void {
  localStorage.removeItem(LOGIN_AGREEMENT_STORAGE_KEY)
  agreementAccepted.value = false
  showAgreementModal.value = false
  appStore.showWarning(t('legal.loginAgreementPrompt.loginRejectedWarning'))
}

// ==================== Turnstile Handlers ====================

function onTurnstileVerify(token: string, randstr = ''): void {
  turnstileToken.value = token
  tencentCaptchaRandstr.value = randstr
  errors.turnstile = ''
}

function onTurnstileExpire(): void {
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  errors.turnstile = t('auth.turnstileExpired')
}

function onTurnstileError(): void {
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  errors.turnstile = t('auth.turnstileFailed')
}

function resetCaptchaProof(): void {
  turnstileRef.value?.reset()
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  errors.turnstile = ''
}

async function acquireActionProof(): Promise<boolean> {
  if (!actionCaptchaEnabled.value) return true

  const proof = await turnstileRef.value?.verifyAction()
  if (!proof) return false

  turnstileToken.value = proof.token
  tencentCaptchaRandstr.value = proof.randstr
  return true
}

// ==================== Validation ====================

function validateForm(): boolean {
  // Reset errors
  errors.email = ''
  errors.password = ''
  errors.turnstile = ''

  let isValid = true

  if (agreementGateActive.value) {
    appStore.showWarning(t('legal.loginAgreementPrompt.loginRequiredWarning'))
    if (loginAgreementMode.value !== 'checkbox') {
      showAgreementModal.value = true
    }
    return false
  }

  // Email validation
  if (!formData.email.trim()) {
    errors.email = t('auth.emailRequired')
    isValid = false
  } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(formData.email)) {
    errors.email = t('auth.invalidEmail')
    isValid = false
  }

  // Password validation
  if (!formData.password) {
    errors.password = t('auth.passwordRequired')
    isValid = false
  } else if (formData.password.length < 6) {
    errors.password = t('auth.passwordMinLength')
    isValid = false
  }

  // Turnstile validation
  if (turnstileEnabled.value && !turnstileToken.value) {
    errors.turnstile = t('auth.completeVerification')
    isValid = false
  }

  return isValid
}

// ==================== Form Handlers ====================

async function handleLogin(): Promise<void> {
  // Clear previous error
  errorMessage.value = ''

  // Validate form
  if (!validateForm()) {
    return
  }

  if (!(await acquireActionProof())) {
    return
  }

  isLoading.value = true

  try {
    // Call auth store login（阿里云 captchaVerifyParam 复用 turnstile_token 字段）
    const response = await authStore.login({
      email: formData.email,
      password: formData.password,
      turnstile_token:
        turnstileEnabled.value || aliyunCaptchaEnabled.value ? turnstileToken.value : undefined,
      tencent_captcha_ticket: tencentCaptchaEnabled.value ? turnstileToken.value : undefined,
      tencent_captcha_randstr: tencentCaptchaEnabled.value
        ? tencentCaptchaRandstr.value
        : undefined
    })

    // Check if 2FA is required
    if (isTotp2FARequired(response)) {
      const totpResponse = response as TotpLoginResponse
      totpTempToken.value = totpResponse.temp_token || ''
      totpUserEmailMasked.value = totpResponse.user_email_masked || ''
      show2FAModal.value = true
      isLoading.value = false
      return
    }

    // Show success toast
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))

    // Redirect to dashboard or intended route
    const redirectTo = (router.currentRoute.value.query.redirect as string) || '/dashboard'
    await router.push(redirectTo)
  } catch (error: unknown) {
    errorMessage.value = extractI18nErrorMessage(error, t, 'auth.errors', t('auth.loginFailed'))

    // Also show error toast
    appStore.showError(errorMessage.value)
  } finally {
    if (captchaEnabled.value) {
      resetCaptchaProof()
    }
    isLoading.value = false
  }
}

async function handlePasskeyLogin(): Promise<void> {
  if (agreementGateActive.value) {
    appStore.showWarning(t('legal.loginAgreementPrompt.loginRequiredWarning'))
    if (loginAgreementMode.value !== 'checkbox') {
      showAgreementModal.value = true
    }
    return
  }

  passkeyLoading.value = true
  try {
    let proof: ActionCaptchaRequestProof | undefined
    if (actionCaptchaEnabled.value) {
      const result = await turnstileRef.value?.verifyAction()
      if (!result) return
      proof = tencentCaptchaEnabled.value
        ? {
            tencent_captcha_ticket: result.token,
            tencent_captcha_randstr: result.randstr
          }
        : { turnstile_token: result.token }
    }

    await authStore.loginWithPasskey(proof)
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))
    const redirectTo = (router.currentRoute.value.query.redirect as string) || '/dashboard'
    await router.push(redirectTo)
  } catch (error: unknown) {
    const fallback = error instanceof DOMException && error.name === 'NotAllowedError'
      ? t('auth.passkeyCancelled')
      : t('auth.passkeyFailed')
    errorMessage.value = extractI18nErrorMessage(error, t, 'auth.errors', fallback)
    appStore.showError(errorMessage.value)
  } finally {
    if (actionCaptchaEnabled.value) {
      resetCaptchaProof()
    }
    passkeyLoading.value = false
  }
}

async function handleOAuthStart(request: OAuthLoginStart): Promise<void> {
  if (authActionDisabled.value) return

  if (!actionCaptchaEnabled.value) {
    window.location.href = buildOAuthLoginStartURL(request)
    return
  }

  isLoading.value = true
  try {
    const proof = await turnstileRef.value?.verifyAction()
    if (!proof) return

    const result = await startOAuthLogin(
      request,
      tencentCaptchaEnabled.value
        ? {
            tencent_captcha_ticket: proof.token,
            tencent_captcha_randstr: proof.randstr
          }
        : { turnstile_token: proof.token }
    )
    window.location.href = result.authorize_url
  } catch (error: unknown) {
    errorMessage.value = extractI18nErrorMessage(
      error,
      t,
      'auth.errors',
      t('auth.turnstileFailed')
    )
    appStore.showError(errorMessage.value)
  } finally {
    resetCaptchaProof()
    isLoading.value = false
  }
}

// ==================== 2FA Handlers ====================

async function handle2FAVerify(code: string): Promise<void> {
  if (totpModalRef.value) {
    totpModalRef.value.setVerifying(true)
  }

  try {
    await authStore.login2FA(totpTempToken.value, code)

    // Close modal and show success
    show2FAModal.value = false
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))

    // Redirect to dashboard or intended route
    const redirectTo = (router.currentRoute.value.query.redirect as string) || '/dashboard'
    await router.push(redirectTo)
  } catch (error: unknown) {
    const err = error as { message?: string; response?: { data?: { message?: string } } }
    const message = err.response?.data?.message || err.message || t('profile.totp.loginFailed')

    if (totpModalRef.value) {
      totpModalRef.value.setError(message)
      totpModalRef.value.setVerifying(false)
    }
  }
}

function handle2FACancel(): void {
  show2FAModal.value = false
  totpTempToken.value = ''
  totpUserEmailMasked.value = ''
}
</script>

<style scoped>
.premium-login {
  display: grid;
  min-width: 0;
  gap: 1.75rem;
}

.login-heading {
  display: grid;
  gap: 0.625rem;
}

.login-heading h2 {
  max-width: 24ch;
  color: var(--auth-ink);
  font-size: 2rem;
  font-weight: 600;
  letter-spacing: 0;
  text-wrap: balance;
}

.login-heading p {
  max-width: 48ch;
  color: var(--auth-muted);
  font-size: 0.875rem;
  line-height: 1.5rem;
  text-wrap: pretty;
}

.login-alert {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.625rem;
  padding: 0.75rem;
  border: 1px solid rgba(220, 38, 38, 0.18);
  border-radius: 0.5rem;
  background: rgba(254, 242, 242, 0.82);
  color: #b91c1c;
}

.login-alert :deep(svg) {
  flex-shrink: 0;
  margin-top: 0.125rem;
}

.login-alert p {
  min-width: 0;
  font-size: 0.8125rem;
  line-height: 1.25rem;
  overflow-wrap: anywhere;
}

.login-form {
  display: grid;
  gap: 1.125rem;
}

.login-field {
  display: grid;
  min-width: 0;
  gap: 0.5rem;
}

.login-label-row {
  display: flex;
  min-width: 0;
  align-items: baseline;
  justify-content: space-between;
  gap: 1rem;
}

.login-label {
  color: var(--auth-ink);
  font-size: 0.8125rem;
  font-weight: 600;
}

.login-inline-link {
  flex-shrink: 0;
  color: var(--auth-muted);
  font-size: 0.75rem;
  font-weight: 600;
  text-decoration: none;
}

.login-inline-link:hover,
.login-inline-link:focus-visible {
  color: var(--auth-ink);
}

.login-inline-link:focus-visible {
  border-radius: 0.25rem;
  outline: 2px solid var(--auth-accent);
  outline-offset: 2px;
}

.login-control {
  position: relative;
  display: flex;
  min-width: 0;
  align-items: center;
}

.login-control__icon {
  position: absolute;
  z-index: 2;
  left: 0.875rem;
  flex-shrink: 0;
  color: var(--auth-faint);
  pointer-events: none;
}

.login-input.input {
  min-height: 3rem;
  padding: 0 1rem 0 2.75rem;
  border: 1px solid var(--auth-line-strong);
  border-radius: 0.5rem;
  background: var(--auth-panel);
  box-shadow: inset 0 1px 0 rgba(18, 20, 25, 0.02);
  color: var(--auth-ink);
  font-size: 0.875rem;
}

.login-input.input::placeholder {
  color: var(--auth-faint);
}

.login-input.input:focus {
  border-color: var(--auth-accent);
  outline: 2px solid color-mix(in srgb, var(--auth-accent) 28%, transparent);
  outline-offset: -1px;
  box-shadow: none;
}

.login-input.input:disabled {
  background: var(--auth-visual);
  color: var(--auth-faint);
}

.login-input.input-error {
  border-color: #dc2626;
}

.login-input--password.input {
  padding-right: 3rem;
}

.login-password-toggle {
  position: absolute;
  z-index: 3;
  right: 0;
  display: grid;
  width: 3rem;
  height: 3rem;
  place-items: center;
  border: 0;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--auth-faint);
  cursor: pointer;
}

.login-password-toggle:hover,
.login-password-toggle:focus-visible {
  color: var(--auth-ink);
}

.login-password-toggle:focus-visible {
  outline: 2px solid var(--auth-accent);
  outline-offset: -3px;
}

.login-password-toggle:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.login-field__error {
  color: #dc2626;
  font-size: 0.8125rem;
  line-height: 1.25rem;
}

.login-captcha {
  display: grid;
  min-width: 0;
  gap: 0.5rem;
  overflow: hidden;
  border-radius: 0.5rem;
}

.login-submit.btn {
  min-height: 3rem;
  justify-content: space-between;
  padding: 0 0.75rem 0 1rem;
  border-radius: 0.5rem;
  background: var(--auth-ink);
  box-shadow: none;
  color: var(--auth-panel);
  font-size: 0.875rem;
  font-weight: 650;
  transition: transform 180ms ease;
}

.login-submit.btn:hover:not(:disabled) {
  transform: translateY(-0.125rem);
}

.login-submit.btn:focus-visible {
  outline: 2px solid var(--auth-accent);
  outline-offset: 2px;
  box-shadow: none;
}

.login-submit.btn:disabled {
  background: color-mix(in srgb, var(--auth-ink) 45%, var(--auth-panel));
  color: color-mix(in srgb, var(--auth-panel) 78%, transparent);
}

.login-spinner {
  width: 1rem;
  height: 1rem;
  flex-shrink: 0;
  border: 2px solid currentColor;
  border-right-color: transparent;
  border-radius: 50%;
  animation: login-spin 700ms linear infinite;
}

.login-alternatives {
  display: grid;
  gap: 0.75rem;
  padding-top: 0.25rem;
}

.login-divider {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  gap: 0.75rem;
}

.login-divider > span {
  height: 1px;
  background: var(--auth-line);
}

.login-divider p {
  color: var(--auth-faint);
  font-size: 0.75rem;
  white-space: nowrap;
}

.login-secondary.btn-secondary,
.login-alternatives :deep(.btn-secondary) {
  min-height: 2.75rem;
  padding: 0 0.75rem;
  border: 1px solid var(--auth-line);
  border-radius: 0.5rem;
  background: transparent;
  box-shadow: none;
  color: var(--auth-ink);
  font-size: 0.8125rem;
  font-weight: 600;
}

.login-secondary.btn-secondary:hover:not(:disabled),
.login-alternatives :deep(.btn-secondary:hover:not(:disabled)) {
  border-color: var(--auth-line-strong);
  background: var(--auth-visual);
  box-shadow: none;
}

.login-secondary.btn-secondary:focus-visible,
.login-alternatives :deep(.btn-secondary:focus-visible) {
  outline: 2px solid var(--auth-accent);
  outline-offset: 2px;
  box-shadow: none;
}

.premium-login :deep(#login-agreement-consent) {
  accent-color: var(--auth-ink);
}

.login-registration {
  color: var(--auth-muted);
  font-size: 0.875rem;
  line-height: 1.5rem;
}

.login-registration a {
  color: var(--auth-ink);
  font-weight: 650;
  text-decoration: none;
}

.login-registration a:hover,
.login-registration a:focus-visible {
  text-decoration: underline;
  text-underline-offset: 0.25rem;
}

.login-registration a:focus-visible {
  border-radius: 0.25rem;
  outline: 2px solid var(--auth-accent);
  outline-offset: 2px;
}

@keyframes login-spin {
  to {
    transform: rotate(360deg);
  }
}

.fade-enter-active,
.fade-leave-active {
  transition:
    opacity 180ms ease,
    transform 180ms ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-0.5rem);
}

@media (max-width: 40rem) {
  .premium-login {
    gap: 1.5rem;
  }

  .login-heading h2 {
    font-size: 1.75rem;
  }

  .login-heading p,
  .login-label,
  .login-inline-link,
  .login-input.input,
  .login-submit.btn,
  .login-secondary.btn-secondary,
  .login-alternatives :deep(.btn-secondary),
  .login-registration {
    font-size: 1rem;
  }

  .login-input.input,
  .login-password-toggle,
  .login-submit.btn {
    min-height: 3.25rem;
  }

  .login-password-toggle {
    width: 3.25rem;
    height: 3.25rem;
  }

  .login-input--password.input {
    padding-right: 3.25rem;
  }

  .login-secondary.btn-secondary,
  .login-alternatives :deep(.btn-secondary) {
    min-height: 3rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .login-submit.btn,
  .fade-enter-active,
  .fade-leave-active {
    transition: none;
  }

  .login-spinner {
    animation-duration: 1.4s;
  }
}
</style>

<style>
.dark .login-alert {
  border-color: rgba(248, 113, 113, 0.2);
  background: rgba(127, 29, 29, 0.14);
  color: #fca5a5;
}

.dark .login-input.input {
  box-shadow: none;
}

.dark .login-field__error {
  color: #fca5a5;
}
</style>
