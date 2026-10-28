package service

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"io"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// 自动登录资料与账号凭据分表存储，管理员接口只返回配置状态。
type ExcelAutoRecord struct {
	AccountID     int64
	Ciphertext    string
	Enabled       bool
	Revision      int64
	State         string
	RunID         string
	NextAttemptAt time.Time
	LastError     string
}
type ExcelAutoRepository interface {
	GetExcelAuto(context.Context, int64) (*ExcelAutoRecord, error)
	SaveExcelAuto(context.Context, int64, int64, string, bool) (bool, error)
	ClaimExcelAuto(context.Context, string) (*ExcelAutoRecord, error)
	FinishExcelAuto(context.Context, *ExcelAutoRecord, *Account, string, string, string) (bool, error)
	RetryExcelAuto(context.Context, int64) error
}
type ExcelAutoLogin struct {
	AccountID  int64  `json:"account_id"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
	Workspace  string `json:"workspace"`
	ProxyURL   string `json:"proxy_url,omitempty"`
}
type ExcelAutoInput struct {
	Enabled    bool   `json:"enabled"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
	ClearTOTP  bool   `json:"clear_totp"`
}
type ExcelAutoStatus struct {
	Enabled        bool       `json:"enabled"`
	Configured     bool       `json:"configured"`
	Email          string     `json:"email"`
	TOTPConfigured bool       `json:"totp_configured"`
	State          string     `json:"state"`
	LastError      string     `json:"last_error"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	WorkerReady    bool       `json:"worker_ready"`
}
type ExcelAutoRunner interface {
	Ready() bool
	Login(context.Context, ExcelAutoLogin) (*OpenAITokenInfo, string)
}
type excelAutoWorker struct {
	runner   ExcelAutoRunner
	settings *SettingService
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once
}

func (s *OpenAIOAuthService) autoRepo() (ExcelAutoRepository, error) {
	r, ok := s.excelAccountRepo.(ExcelAutoRepository)
	if !ok || s.excelEncryptor == nil {
		return nil, errors.New("自动授权存储不可用")
	}
	return r, nil
}
func (s *OpenAIOAuthService) autoAccount(ctx context.Context, id int64) (*Account, error) {
	a, err := s.excelAccountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !excelOAuthAccountEligible(a) || a.IsExcelOAuth() || strings.EqualFold(a.GetCredential("plan_type"), "free") {
		return nil, errors.New("自动授权仅支持非免费套餐的普通 Codex OAuth 根账号")
	}
	return a, nil
}
func (s *OpenAIOAuthService) decodeAuto(r *ExcelAutoRecord) (ExcelAutoLogin, error) {
	var v ExcelAutoLogin
	raw, err := s.excelEncryptor.Decrypt(r.Ciphertext)
	if err != nil || json.Unmarshal([]byte(raw), &v) != nil || v.AccountID != r.AccountID {
		return v, errors.New("登录资料解密失败，请重新保存")
	}
	return v, nil
}
func (s *OpenAIOAuthService) ExcelAutoStatus(ctx context.Context, id int64) (*ExcelAutoStatus, error) {
	r, err := s.autoRepo()
	if err != nil {
		return nil, err
	}
	if _, err = s.autoAccount(ctx, id); err != nil {
		return nil, err
	}
	record, err := r.GetExcelAuto(ctx, id)
	if err != nil {
		return nil, err
	}
	view := &ExcelAutoStatus{State: "disabled", WorkerReady: s.excelAuto != nil && s.excelAuto.runner.Ready()}
	if record == nil {
		return view, nil
	}
	view.Enabled, view.State, view.LastError = record.Enabled, record.State, excelAutoErrorMessage(record.LastError)
	if record.State == "failed" || record.State == "queued" {
		view.NextAttemptAt = &record.NextAttemptAt
	}
	if record.Ciphertext != "" {
		v, err := s.decodeAuto(record)
		if err != nil {
			return nil, err
		}
		view.Configured, view.Email, view.TOTPConfigured = v.Password != "", v.Email, v.TOTPSecret != ""
	}
	return view, nil
}
func (s *OpenAIOAuthService) SaveExcelAuto(ctx context.Context, id int64, input ExcelAutoInput) error {
	r, err := s.autoRepo()
	if err != nil {
		return err
	}
	a, err := s.autoAccount(ctx, id)
	if err != nil {
		return err
	}
	record, err := r.GetExcelAuto(ctx, id)
	if err != nil {
		return err
	}
	revision := int64(0)
	v := ExcelAutoLogin{AccountID: id}
	if record != nil {
		revision = record.Revision
		if record.Ciphertext != "" {
			v, err = s.decodeAuto(record)
			if err != nil && input.Enabled {
				return err
			}
		}
	}
	if !input.Enabled {
		// 关闭时清除保存的登录秘密，同时使正在运行的任务失效；已取得的授权保留。
		ok, err := r.SaveExcelAuto(ctx, id, revision, "", false)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("配置已变更，请刷新后重试")
		}
		return nil
	}
	email := strings.TrimSpace(input.Email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 320 {
		return errors.New("请输入有效的登录邮箱")
	}
	if email != v.Email && input.Password == "" {
		return errors.New("更换登录邮箱时请重新填写密码")
	}
	v.Email = email
	if input.Password != "" {
		v.Password = input.Password
	}
	if v.Password == "" || len(v.Password) > 4096 {
		return errors.New("请填写登录密码")
	}
	if input.ClearTOTP {
		v.TOTPSecret = ""
	}
	if input.TOTPSecret != "" {
		secret := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(input.TOTPSecret), " ", ""))
		decoded, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(secret, "="))
		if e != nil || len(decoded) < 10 || len(decoded) > 128 {
			return errors.New("请填写 Base32 格式的长期 TOTP 密钥，不是六位验证码")
		}
		v.TOTPSecret = strings.TrimRight(secret, "=")
	}
	v.Workspace = a.GetChatGPTAccountID()
	v.ProxyURL = ""
	if v.Workspace == "" {
		return errors.New("账号缺少工作区身份，请先更新账号授权")
	}
	raw, _ := json.Marshal(v)
	ciphertext, err := s.excelEncryptor.Encrypt(string(raw))
	if err != nil {
		return errors.New("登录资料加密失败")
	}
	ok, err := r.SaveExcelAuto(ctx, id, revision, ciphertext, true)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("配置已变更，请刷新后重试")
	}
	return nil
}

func (s *OpenAIOAuthService) StartExcelAuto(settings *SettingService, runner ExcelAutoRunner) {
	ctx, cancel := context.WithCancel(context.Background())
	w := &excelAutoWorker{runner: runner, settings: settings, cancel: cancel, done: make(chan struct{})}
	s.excelAuto = w
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			s.runExcelAutoCycle(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *OpenAIOAuthService) stopExcelAuto() {
	if w := s.excelAuto; w != nil {
		w.once.Do(w.cancel)
		select {
		case <-w.done:
		case <-time.After(5 * time.Second):
		}
	}
}
func (s *OpenAIOAuthService) runExcelAutoCycle(ctx context.Context) {
	w := s.excelAuto
	if w == nil || !w.runner.Ready() {
		return
	}
	r, err := s.autoRepo()
	if err != nil {
		return
	}
	// 单实例串行处理，跨实例由数据库租约互斥；每批最多处理 50 个账号。
	for i := 0; i < 50 && ctx.Err() == nil; i++ {
		cfg, err := w.settings.GetExcelBPSConfig(ctx)
		if err != nil || !cfg.Enabled {
			return
		}
		job, err := r.ClaimExcelAuto(ctx, uuid.NewString())
		if err != nil || job == nil {
			return
		}
		s.runExcelAutoJob(ctx, r, job)
	}
}
func (s *OpenAIOAuthService) runExcelAutoJob(parent context.Context, r ExcelAutoRepository, job *ExcelAutoRecord) {
	ctx, cancel := context.WithTimeout(parent, 11*time.Minute)
	defer cancel()
	reason := "login_failed"
	var account *Account
	var old, replacement string
	defer func() {
		finish, c := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer c()
		_, _ = r.FinishExcelAuto(finish, job, account, old, replacement, reason)
	}()
	login, err := s.decodeAuto(job)
	if err != nil {
		reason = "login_config_invalid"
		return
	}
	if _, err = s.autoAccount(ctx, job.AccountID); err != nil {
		reason = "account_changed"
		return
	}
	account, old, _, err = s.readExcelGrantForReplacement(ctx, job.AccountID)
	if err != nil || !account.IsExcelBPSEnabled() {
		reason = "account_changed"
		return
	}
	// 已经手动授权且访问令牌仍有效时直接复用，避免重复登录。
	if _, saved, token, e := s.readExcelGrant(ctx, job.AccountID); e == nil && saved == old && token != nil && token.ExpiresAt > time.Now().Add(time.Minute).Unix() && job.LastError != "refresh_failed" {
		replacement, reason = old, ""
		return
	}
	if login.Workspace != account.GetChatGPTAccountID() {
		reason = "workspace_mismatch"
		return
	}
	if account.ProxyID != nil {
		proxy, e := s.proxyRepo.GetByID(ctx, *account.ProxyID)
		if e != nil || proxy == nil {
			reason = "proxy_unavailable"
			return
		}
		login.ProxyURL = proxy.URL()
	}
	info, code := s.excelAuto.runner.Login(ctx, login)
	if code != "" {
		reason = code
		return
	}
	if ctx.Err() != nil {
		reason = "cancelled"
		return
	}
	// 网络登录期间主令牌可能正常轮换；重新读取并验证身份后保存。
	fresh, err := s.autoAccount(ctx, job.AccountID)
	if err != nil || !fresh.IsExcelBPSEnabled() {
		reason = "account_changed"
		return
	}
	if fresh.GetCredential("client_id") != account.GetCredential("client_id") || validateExcelOAuthIdentity(fresh, info, false) != nil {
		reason = "identity_mismatch"
		return
	}
	cfg, err := s.excelAuto.settings.GetExcelBPSConfig(ctx)
	if err != nil || !cfg.Enabled {
		reason = "disabled"
		return
	}
	account = fresh
	raw, err := json.Marshal(excelOAuthGrant{AccountID: account.ID, Token: *info})
	if err != nil {
		return
	}
	replacement, err = s.excelEncryptor.Encrypt(string(raw))
	if err != nil {
		reason = "storage_failed"
		return
	}
	reason = ""
}
func excelAutoErrorMessage(code string) string {
	switch code {
	case "":
		return ""
	case "verification_required":
		return "官方要求额外安全验证，请改用手动授权"
	case "password_rejected":
		return "登录密码被拒绝，请检查登录资料"
	case "totp_rejected":
		return "TOTP 验证失败，请检查长期密钥"
	case "workspace_mismatch", "identity_mismatch":
		return "登录身份或工作区不匹配，请检查账号与登录资料"
	case "proxy_unavailable":
		return "账号绑定的代理不可用"
	case "login_config_invalid":
		return "登录资料无效，请重新保存"
	case "account_changed", "disabled":
		return "账号或开关已变更，未保存本次授权"
	case "cancelled":
		return "任务已取消，稍后自动重试"
	case "timeout":
		return "登录超时，稍后自动重试"
	case "runtime_error":
		return "登录运行时异常，请检查部署"
	default:
		return "自动授权失败，30 分钟后重试；可使用手动授权"
	}
}

// 子进程仅通过管道接收秘密，输出有界且不会写入日志。
type ExcelProcessRunner struct{ Root string }

func (r ExcelProcessRunner) Ready() bool {
	for _, file := range []string{"python", "node", "runner.py", "ready"} {
		if _, err := os.Stat(filepath.Join(r.Root, file)); err != nil {
			return false
		}
	}
	return true
}

type excelBoundedOutput struct{ bytes.Buffer }

func (b *excelBoundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 128*1024 {
		return 0, errors.New("登录结果超出上限")
	}
	return b.Buffer.Write(p)
}
func (r ExcelProcessRunner) Login(ctx context.Context, input ExcelAutoLogin) (*OpenAITokenInfo, string) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, "login_config_invalid"
	}
	cmd := exec.CommandContext(ctx, filepath.Join(r.Root, "python"), filepath.Join(r.Root, "runner.py"))
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Stderr = io.Discard
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/tmp", "EXCEL_PYTHON=" + filepath.Join(r.Root, "python"), "EXCEL_NODE=" + filepath.Join(r.Root, "node")}
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 3 * time.Second
	var output excelBoundedOutput
	cmd.Stdout = &output
	err = cmd.Run()
	var result struct {
		Token *OpenAITokenInfo `json:"token"`
		Error string           `json:"error"`
	}
	if ctx.Err() != nil {
		return nil, "cancelled"
	}
	if json.Unmarshal(output.Bytes(), &result) != nil {
		return nil, "runtime_error"
	}
	if result.Error != "" { // 仅接收固定错误码，绝不持久化外部异常正文。
		switch result.Error {
		case "verification_required", "password_rejected", "totp_rejected", "workspace_mismatch", "login_config_invalid", "cancelled", "timeout":
			return nil, result.Error
		}
		return nil, "login_failed"
	}
	if err != nil || result.Token == nil {
		return nil, "runtime_error"
	}
	return result.Token, ""
}
