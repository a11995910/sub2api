package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// Excel 授权只由专用接口写入；普通账号编辑、主令牌刷新不能覆盖它。
type ExcelOAuthGrantRepository interface {
	GetExcelOAuthGrant(context.Context, int64) (string, error)
	CompareAndSwapExcelOAuthGrant(context.Context, *Account, string, string) (bool, error)
}

type excelOAuthGrant struct {
	AccountID int64           `json:"account_id"`
	Token     OpenAITokenInfo `json:"token"`
}

type ExcelOAuthStatus struct {
	Authorized bool  `json:"authorized"`
	Primary    bool  `json:"primary"`
	ExpiresAt  int64 `json:"expires_at,omitempty"`
}

var excelOAuthLocks sync.Map
var errExcelOAuthRefreshBusy = errors.New("Excel 授权正在刷新，请稍后重试")

func (s *OpenAIOAuthService) SetExcelOAuthDependencies(repo AccountRepository, encryptor SecretEncryptor, cache OpenAITokenCache) {
	s.excelAccountRepo, s.excelEncryptor, s.excelTokenCache = repo, encryptor, cache
}

func (s *OpenAIOAuthService) excelRepository() (ExcelOAuthGrantRepository, error) {
	repo, ok := s.excelAccountRepo.(ExcelOAuthGrantRepository)
	if !ok || s.excelEncryptor == nil {
		return nil, errors.New("Excel 授权存储不可用")
	}
	return repo, nil
}

func excelOAuthAccountEligible(a *Account) bool {
	return a != nil && a.Platform == PlatformOpenAI && a.Type == AccountTypeOAuth && !a.IsShadow() && !a.IsOpenAIAgentIdentity() && !a.IsOpenAIPersonalAccessToken()
}

// 本地锁和 Redis 锁共同保护轮换令牌；网络超时及持久化在锁租期内完成。
func (s *OpenAIOAuthService) lockExcelGrant(ctx context.Context, id int64) (func(), error) {
	key := fmt.Sprintf("openai:excel:account:%d", id)
	value, _ := excelOAuthLocks.LoadOrStore(id, newContextMutex())
	mu := value.(*contextMutex)
	if err := mu.Lock(ctx); err != nil {
		return nil, err
	}
	if s.excelTokenCache != nil {
		acquired, err := s.excelTokenCache.AcquireRefreshLock(ctx, key, time.Minute)
		if err != nil || !acquired {
			mu.Unlock()
			if err != nil {
				return nil, errors.New("Excel 授权刷新锁不可用")
			}
			return nil, errExcelOAuthRefreshBusy
		}
	}
	return func() {
		defer mu.Unlock()
		if s.excelTokenCache != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_ = s.excelTokenCache.ReleaseRefreshLock(cleanup, key)
		}
	}, nil
}

// 声明只用于核对通过官方 TLS 令牌交换取得的结果，不代替 JWT 签名认证。
func validateExcelOAuthIdentity(a *Account, info *OpenAITokenInfo, allowExpired bool) error {
	if !excelOAuthAccountEligible(a) || info == nil || info.ClientID != openai.ExcelClientID || info.RefreshToken == "" {
		return errors.New("缺少有效的 Excel OAuth 授权")
	}
	parts := strings.Split(info.AccessToken, ".")
	if len(parts) != 3 {
		return errors.New("Excel 访问令牌格式无效")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("Excel 访问令牌格式无效")
	}
	var client struct {
		ClientID string `json:"client_id"`
		AZP      string `json:"azp"`
	}
	if json.Unmarshal(payload, &client) != nil || (client.ClientID != openai.ExcelClientID && (client.ClientID != "" || client.AZP != openai.ExcelClientID)) {
		return errors.New("Excel OAuth 客户端不匹配")
	}
	at, err := openai.DecodeIDToken(info.AccessToken)
	if err != nil || at.OpenAIAuth == nil || at.Exp <= 0 || (!allowExpired && at.Exp <= time.Now().Unix()) {
		return errors.New("Excel 访问令牌身份或有效期无效")
	}
	id, err := openai.DecodeIDToken(info.IDToken)
	if err != nil {
		return errors.New("Excel 身份令牌无效")
	}
	audience := false
	for _, aud := range id.Aud {
		audience = audience || aud == openai.ExcelClientID
	}
	if !audience {
		return errors.New("Excel 身份令牌客户端不匹配")
	}
	workspace, user := a.GetChatGPTAccountID(), a.GetCredential("chatgpt_user_id")
	for _, key := range []string{"access_token", "id_token"} {
		claims, decodeErr := openai.DecodeIDToken(a.GetCredential(key))
		if decodeErr == nil && claims.OpenAIAuth != nil {
			if workspace == "" {
				workspace = claims.OpenAIAuth.ChatGPTAccountID
			}
			if user == "" {
				user = claims.OpenAIAuth.ChatGPTUserID
				if user == "" {
					user = claims.OpenAIAuth.UserID
				}
			}
		}
	}
	excelUser := at.OpenAIAuth.ChatGPTUserID
	if excelUser == "" {
		excelUser = at.OpenAIAuth.UserID
	}
	if excelUser == "" {
		excelUser = id.GetUserInfo().ChatGPTUserID
	}
	if workspace == "" || user == "" || workspace != at.OpenAIAuth.ChatGPTAccountID || user != excelUser {
		return errors.New("Excel 授权与当前账号的用户或工作区不一致")
	}
	info.ChatGPTAccountID, info.ChatGPTUserID, info.ExpiresAt = workspace, user, at.Exp
	return nil
}

func (s *OpenAIOAuthService) readExcelGrant(ctx context.Context, accountID int64) (*Account, string, *OpenAITokenInfo, error) {
	repo, err := s.excelRepository()
	if err != nil {
		return nil, "", nil, err
	}
	a, err := s.excelAccountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, "", nil, err
	}
	if !excelOAuthAccountEligible(a) {
		return nil, "", nil, errors.New("此账号不支持 Excel 授权")
	}
	ciphertext, err := repo.GetExcelOAuthGrant(ctx, accountID)
	if err != nil || ciphertext == "" {
		return a, ciphertext, nil, err
	}
	raw, err := s.excelEncryptor.Decrypt(ciphertext)
	if err != nil {
		return nil, "", nil, errors.New("Excel 授权解密失败")
	}
	var grant excelOAuthGrant
	if json.Unmarshal([]byte(raw), &grant) != nil || grant.AccountID != accountID {
		return nil, "", nil, errors.New("Excel 授权存储无效")
	}
	if err := validateExcelOAuthIdentity(a, &grant.Token, true); err != nil {
		return nil, "", nil, err
	}
	return a, ciphertext, &grant.Token, nil
}

func (s *OpenAIOAuthService) ExcelAuthorizationStatus(ctx context.Context, id int64) (*ExcelOAuthStatus, error) {
	a, _, token, err := s.readExcelGrant(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.IsExcelOAuth() {
		return &ExcelOAuthStatus{Authorized: true, Primary: true}, nil
	}
	status := &ExcelOAuthStatus{}
	if token != nil {
		status.Authorized, status.ExpiresAt = true, token.ExpiresAt
	}
	return status, nil
}

func (s *OpenAIOAuthService) saveExcelGrant(ctx context.Context, a *Account, old string, info *OpenAITokenInfo) error {
	repo, err := s.excelRepository()
	if err != nil {
		return err
	}
	if err := validateExcelOAuthIdentity(a, info, false); err != nil {
		return err
	}
	raw, err := json.Marshal(excelOAuthGrant{AccountID: a.ID, Token: *info})
	if err != nil {
		return err
	}
	ciphertext, err := s.excelEncryptor.Encrypt(string(raw))
	if err != nil {
		return errors.New("Excel 授权加密失败")
	}
	// 令牌已轮换，即使浏览器断开也完成有界持久化。
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	changed, err := repo.CompareAndSwapExcelOAuthGrant(persist, a, old, ciphertext)
	if err != nil {
		return errors.New("Excel 授权保存失败")
	}
	if !changed {
		fresh, readErr := s.excelAccountRepo.GetByID(persist, a.ID)
		if readErr != nil || validateExcelOAuthIdentity(fresh, info, false) != nil || fresh.GetCredential("client_id") != a.GetCredential("client_id") {
			return errors.New("账号身份已变更，请重新授权")
		}
		changed, err = repo.CompareAndSwapExcelOAuthGrant(persist, fresh, old, ciphertext)
		if err != nil || !changed {
			return errors.New("账号或授权已变更，请重新授权")
		}
	}
	return nil
}

func (s *OpenAIOAuthService) ExchangeExcelAccountCode(ctx context.Context, accountID int64, input *OpenAIExchangeCodeInput) error {
	session, ok := s.sessionStore.Get(input.SessionID)
	if !ok || session.ClientID != openai.ExcelClientID {
		return errors.New("请先生成此账号的 Excel 授权链接")
	}
	unlock, err := s.lockExcelGrant(ctx, accountID)
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	a, old, _, err := s.readExcelGrantForReplacement(ctx, accountID)
	if err != nil {
		return err
	}
	input.ProxyID = a.ProxyID
	info, err := s.ExchangeCode(ctx, input)
	if err != nil {
		return errors.New("Excel 授权码交换失败，请重新生成链接后重试")
	}
	return s.saveExcelGrant(ctx, a, old, info)
}

// 重新授权允许替换过期、被拒绝或不再匹配账号的旧授权。
func (s *OpenAIOAuthService) readExcelGrantForReplacement(ctx context.Context, id int64) (*Account, string, *OpenAITokenInfo, error) {
	repo, err := s.excelRepository()
	if err != nil {
		return nil, "", nil, err
	}
	a, err := s.excelAccountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, "", nil, err
	}
	if !excelOAuthAccountEligible(a) || a.IsExcelOAuth() {
		return nil, "", nil, errors.New("此入口仅用于普通 OpenAI OAuth 账号的独立 Excel 授权")
	}
	old, err := repo.GetExcelOAuthGrant(ctx, id)
	return a, old, nil, err
}

func (s *OpenAIOAuthService) ExcelAccessToken(ctx context.Context, id int64) (string, error) {
	_, _, token, err := s.readExcelGrant(ctx, id)
	if err != nil {
		return "", err
	}
	if token == nil {
		return "", errors.New("请在账号设置中完成独立 Excel 授权")
	}
	if token.ExpiresAt > time.Now().Add(openAITokenRefreshSkew).Unix() {
		return token.AccessToken, nil
	}
	unlock, err := s.lockExcelGrant(ctx, id)
	if err != nil {
		if errors.Is(err, errExcelOAuthRefreshBusy) && token.ExpiresAt > time.Now().Unix() {
			return token.AccessToken, nil
		}
		return "", err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	a, old, token, err := s.readExcelGrant(ctx, id)
	if err != nil {
		return "", err
	}
	if token == nil {
		return "", errors.New("Excel 授权已被移除")
	}
	if token.ExpiresAt > time.Now().Add(openAITokenRefreshSkew).Unix() {
		return token.AccessToken, nil
	}
	proxy := ""
	if a.ProxyID != nil {
		p, e := s.proxyRepo.GetByID(ctx, *a.ProxyID)
		if e != nil || p == nil {
			return "", errors.New("Excel 授权代理不可用")
		}
		proxy = p.URL()
	}
	response, err := s.oauthClient.RefreshTokenWithClientID(ctx, token.RefreshToken, proxy, openai.ExcelClientID)
	if err != nil || response == nil {
		if err != nil && strings.Contains(err.Error(), "invalid_grant") {
			if repo, e := s.autoRepo(); e == nil {
				_ = repo.RetryExcelAuto(ctx, id)
			}
		}
		return "", errors.New("Excel 授权刷新失败，请检查自动授权状态或重新授权")
	}
	token.AccessToken = response.AccessToken
	if response.RefreshToken != "" {
		token.RefreshToken = response.RefreshToken
	}
	if response.IDToken != "" {
		token.IDToken = response.IDToken
	}
	if err := s.saveExcelGrant(ctx, a, old, token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func (s *OpenAIGatewayService) getExcelBPSAccessToken(ctx context.Context, a *Account) (string, error) {
	if a.IsExcelOAuth() {
		if s.openAITokenProvider != nil {
			return s.openAITokenProvider.GetAccessToken(ctx, a)
		}
		token := a.GetOpenAIAccessToken()
		if token == "" {
			return "", errors.New("Excel OAuth 凭据不可用")
		}
		return token, nil
	}
	if s.openAITokenProvider == nil || s.openAITokenProvider.openAIOAuthService == nil {
		return "", errors.New("Excel 授权服务不可用")
	}
	return s.openAITokenProvider.openAIOAuthService.ExcelAccessToken(ctx, a.ID)
}
