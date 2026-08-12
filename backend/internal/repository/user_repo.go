package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/ent/authidentitychannel"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/identityadoptiondecision"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/ent/userallowedgroup"
	"github.com/Wei-Shaw/sub2api/ent/userblockedgroup"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"

	entsql "entgo.io/ent/dialect/sql"
)

type userRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

var _ service.RedeemUserAdjustmentRepository = (*userRepository)(nil)

func NewUserRepository(client *dbent.Client, sqlDB *sql.DB) service.UserRepository {
	return newUserRepositoryWithSQL(client, sqlDB)
}

func newUserRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *userRepository {
	return &userRepository{client: client, sql: sqlq}
}

func (r *userRepository) Create(ctx context.Context, userIn *service.User) error {
	return r.create(ctx, userIn, false, "")
}

// CreateWithEmailAliasGuard 见 service.UserRepository：在邮箱唯一性锁内复查收件箱身份，
// 供注册路径使用。
func (r *userRepository) CreateWithEmailAliasGuard(ctx context.Context, userIn *service.User) error {
	return r.create(ctx, userIn, true, "")
}

// CountUsersByEmailDomain 统计指定可注册主域名及其子域名下的未删除用户。
func (r *userRepository) CountUsersByEmailDomain(ctx context.Context, domain string) (int, error) {
	return countUsersByEmailDomainWithClient(ctx, clientFromContext(ctx, r.client), domain)
}

// CreateWithEmailAliasGuardAndDomainLimit 串行化非白名单域名的注册请求，
// 并在用户写入的同一事务内复查域名额度。
func (r *userRepository) CreateWithEmailAliasGuardAndDomainLimit(ctx context.Context, userIn *service.User, domain string) error {
	return r.create(ctx, userIn, true, normalizeEmailDomain(domain))
}

func (r *userRepository) create(ctx context.Context, userIn *service.User, guardEmailAlias bool, domainLimit string) error {
	if userIn == nil {
		return nil
	}

	// 统一使用 ent 的事务：保证用户与允许分组的更新原子化，
	// 并避免基于 *sql.Tx 手动构造 ent client 导致的 ExecQuerier 断言错误。
	txCtx := ctx
	var txClient *dbent.Client
	var tx *dbent.Tx
	if existingTx := dbent.TxFromContext(ctx); existingTx != nil {
		// 外层服务已开启事务时复用同一个 tx client，避免本方法悄悄开启第二个事务。
		txClient = existingTx.Client()
	} else {
		var err error
		tx, err = r.client.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		txClient = tx.Client()
		txCtx = dbent.NewTxContext(ctx, tx)
	}

	lockKeys := []string{normalizedEmailUniquenessLockKey(userIn.Email)}
	if guardEmailAlias {
		// 别名变体的字面量不同，唯一索引无法兜底；用收件箱身份锁把同一收件箱的并发注册串行化。
		lockKeys = append(lockKeys, emailAliasUniquenessLockKey(userIn.Email))
	}
	if domainLimit != "" {
		lockKeys = append(lockKeys, registrationEmailDomainLockKey(domainLimit))
	}
	releaseEmailLock, err := lockRepositoryScopedKeys(
		txCtx,
		txClient,
		txAwareSQLExecutor(txCtx, r.sql, r.client),
		lockKeys...,
	)
	if err != nil {
		return err
	}
	defer releaseEmailLock()

	if domainLimit != "" {
		count, err := countUsersByEmailDomainWithClient(txCtx, txClient, domainLimit)
		if err != nil {
			return err
		}
		if count > 0 {
			return service.ErrEmailDomainRegistrationLimit
		}
	}

	if err := ensureNormalizedEmailAvailableWithClient(txCtx, txClient, 0, userIn.Email); err != nil {
		return err
	}

	if guardEmailAlias {
		aliasExists, err := existsByEmailAliasWithClient(txCtx, txClient, userIn.Email)
		if err != nil {
			return err
		}
		if aliasExists {
			return service.ErrEmailExists
		}
	}

	created, err := txClient.User.Create().
		SetEmail(userIn.Email).
		SetUsername(userIn.Username).
		SetNotes(userIn.Notes).
		SetPasswordHash(userIn.PasswordHash).
		SetRole(userIn.Role).
		SetBalance(userIn.Balance).
		SetConcurrency(userIn.Concurrency).
		SetStatus(userIn.Status).
		SetSignupSource(userSignupSourceOrDefault(userIn.SignupSource)).
		SetNillableLastLoginAt(userIn.LastLoginAt).
		SetNillableLastActiveAt(userIn.LastActiveAt).
		SetRpmLimit(userIn.RPMLimit).
		Save(txCtx)
	if err != nil {
		return translatePersistenceError(err, nil, service.ErrEmailExists)
	}

	if err := r.syncUserAllowedGroupsWithClient(txCtx, txClient, created.ID, userIn.AllowedGroups); err != nil {
		return err
	}
	if err := r.syncUserBlockedGroupsWithClient(txCtx, txClient, created.ID, userIn.BlockedGroups); err != nil {
		return err
	}
	if err := ensureEmailAuthIdentityWithClient(txCtx, txClient, created.ID, created.Email, "user_repo_create"); err != nil {
		return err
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	applyUserEntityToService(userIn, created)
	return nil
}

func (r *userRepository) GetByID(ctx context.Context, id int64) (*service.User, error) {
	m, err := r.client.User.Query().Where(dbuser.IDEQ(id)).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrUserNotFound, nil)
	}

	out := userEntityToService(m)
	groups, err := r.loadAllowedGroups(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	if v, ok := groups[id]; ok {
		out.AllowedGroups = v
	}
	blockedGroups, err := r.loadBlockedGroups(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	out.BlockedGroups = blockedGroups[id]
	return out, nil
}

func (r *userRepository) GetByIDIncludeDeleted(ctx context.Context, id int64) (*service.User, error) {
	ctx = mixins.SkipSoftDelete(ctx)
	m, err := r.client.User.Query().Where(dbuser.IDEQ(id)).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	out := userEntityToService(m)
	groups, err := r.loadAllowedGroups(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	if v, ok := groups[id]; ok {
		out.AllowedGroups = v
	}
	blockedGroups, err := r.loadBlockedGroups(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	out.BlockedGroups = blockedGroups[id]
	return out, nil
}

func (r *userRepository) GetByEmail(ctx context.Context, email string) (*service.User, error) {
	matches, err := r.client.User.Query().
		Where(userEmailLookupPredicate(email)).
		Order(dbent.Asc(dbuser.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, service.ErrUserNotFound
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("normalized email lookup matched multiple users for %q", strings.TrimSpace(email))
	}
	m := matches[0]

	out := userEntityToService(m)
	groups, err := r.loadAllowedGroups(ctx, []int64{m.ID})
	if err != nil {
		return nil, err
	}
	if v, ok := groups[m.ID]; ok {
		out.AllowedGroups = v
	}
	blockedGroups, err := r.loadBlockedGroups(ctx, []int64{m.ID})
	if err != nil {
		return nil, err
	}
	out.BlockedGroups = blockedGroups[m.ID]
	return out, nil
}

func (r *userRepository) Update(ctx context.Context, userIn *service.User, fields service.UserUpdateFields) error {
	if userIn == nil {
		return nil
	}
	// 空掩码代表调用方不改任何列，直接返回，避免产生一次无意义的整行写。
	if fields.IsEmpty() {
		return nil
	}

	// 使用 ent 事务包裹用户更新与 allowed_groups 同步，避免跨层事务不一致。
	txCtx := ctx
	var txClient *dbent.Client
	var tx *dbent.Tx
	if existingTx := dbent.TxFromContext(ctx); existingTx != nil {
		// 外层服务已开启事务时复用同一个 tx client，保证用户字段与授权集合原子提交。
		txClient = existingTx.Client()
	} else {
		var err error
		tx, err = r.client.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		txClient = tx.Client()
		txCtx = dbent.NewTxContext(ctx, tx)
	}

	// 邮箱唯一性锁与查重只在本次确实要改邮箱时才做：不改邮箱的更新既不需要
	// 串行化，也不该因为快照里的旧邮箱已被他人占用而报 ErrEmailExists。
	if fields.Email {
		releaseEmailLock, err := lockRepositoryScopedKeys(
			txCtx,
			txClient,
			txAwareSQLExecutor(txCtx, r.sql, r.client),
			normalizedEmailUniquenessLockKey(userIn.Email),
		)
		if err != nil {
			return err
		}
		defer releaseEmailLock()

		if err := ensureNormalizedEmailAvailableWithClient(txCtx, txClient, userIn.ID, userIn.Email); err != nil {
			return err
		}
	}

	existing, err := clientFromContext(txCtx, txClient).User.Get(txCtx, userIn.ID)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	oldEmail := existing.Email
	if existing.Role == service.RoleAdmin && existing.Status == service.StatusActive &&
		(userIn.Role != service.RoleAdmin || userIn.Status != service.StatusActive) {
		releaseAdminLock, lockErr := lockRepositoryScopedKeys(
			txCtx,
			txClient,
			txAwareSQLExecutor(txCtx, r.sql, r.client),
			"users:active-admin-role-change",
		)
		if lockErr != nil {
			return lockErr
		}
		defer releaseAdminLock()

		otherActiveAdmins, countErr := txClient.User.Query().
			Where(
				dbuser.IDNEQ(userIn.ID),
				dbuser.RoleEQ(service.RoleAdmin),
				dbuser.StatusEQ(service.StatusActive),
			).
			Count(txCtx)
		if countErr != nil {
			return fmt.Errorf("count other active admins: %w", countErr)
		}
		if otherActiveAdmins == 0 {
			return errors.New("cannot demote the last admin user")
		}
	}

	updateOp := txClient.User.UpdateOneID(userIn.ID)
	if fields.Email {
		updateOp = updateOp.SetEmail(userIn.Email)
	}
	if fields.Username {
		updateOp = updateOp.SetUsername(userIn.Username)
	}
	if fields.Notes {
		updateOp = updateOp.SetNotes(userIn.Notes)
	}
	if fields.PasswordHash {
		updateOp = updateOp.SetPasswordHash(userIn.PasswordHash)
	}
	if fields.Role {
		updateOp = updateOp.SetRole(userIn.Role)
	}
	if fields.Concurrency {
		updateOp = updateOp.SetConcurrency(userIn.Concurrency)
	}
	if fields.RPMLimit {
		updateOp = updateOp.SetRpmLimit(userIn.RPMLimit)
	}
	if fields.Status {
		updateOp = updateOp.SetStatus(userIn.Status)
	}
	if fields.BalanceNotifySettings {
		updateOp = updateOp.
			SetBalanceNotifyEnabled(userIn.BalanceNotifyEnabled).
			SetBalanceNotifyThresholdType(userIn.BalanceNotifyThresholdType).
			SetNillableBalanceNotifyThreshold(userIn.BalanceNotifyThreshold)
		if userIn.BalanceNotifyThreshold == nil {
			updateOp = updateOp.ClearBalanceNotifyThreshold()
		}
	}
	if fields.BalanceNotifyExtraEmails {
		updateOp = updateOp.SetBalanceNotifyExtraEmails(marshalExtraEmails(userIn.BalanceNotifyExtraEmails))
	}
	if fields.SignupSource && userIn.SignupSource != "" {
		updateOp = updateOp.SetSignupSource(userIn.SignupSource)
	}
	if fields.LastLoginAt && userIn.LastLoginAt != nil {
		updateOp = updateOp.SetLastLoginAt(*userIn.LastLoginAt)
	}
	if fields.LastActiveAt && userIn.LastActiveAt != nil {
		updateOp = updateOp.SetLastActiveAt(*userIn.LastActiveAt)
	}
	updated, err := updateOp.Save(txCtx)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserNotFound, service.ErrEmailExists)
	}

	if fields.AllowedGroups {
		if err := r.syncUserAllowedGroupsWithClient(txCtx, txClient, updated.ID, userIn.AllowedGroups); err != nil {
			return err
		}
	}
	if fields.BlockedGroups {
		if err := r.syncUserBlockedGroupsWithClient(txCtx, txClient, updated.ID, userIn.BlockedGroups); err != nil {
			return err
		}
	}
	// 始终以库中的邮箱为准补齐 email 身份：未改邮箱时 updated.Email == oldEmail，
	// 这里退化为幂等的身份补写，与改邮箱前的行为一致。
	if err := replaceEmailAuthIdentityWithClient(txCtx, txClient, updated.ID, oldEmail, updated.Email, "user_repo_update"); err != nil {
		return err
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	userIn.UpdatedAt = updated.UpdatedAt
	return nil
}

func ensureEmailAuthIdentityWithClient(ctx context.Context, client *dbent.Client, userID int64, email string, source string) error {
	client = clientFromContext(ctx, client)
	if client == nil || userID <= 0 {
		return nil
	}

	subject := normalizeEmailAuthIdentitySubject(email)
	if subject == "" {
		return nil
	}

	if err := client.AuthIdentity.Create().
		SetUserID(userID).
		SetProviderType("email").
		SetProviderKey("email").
		SetProviderSubject(subject).
		SetVerifiedAt(time.Now().UTC()).
		SetMetadata(map[string]any{"source": source}).
		OnConflictColumns(
			authidentity.FieldProviderType,
			authidentity.FieldProviderKey,
			authidentity.FieldProviderSubject,
		).
		DoNothing().
		Exec(ctx); err != nil {
		if !isSQLNoRowsError(err) {
			return err
		}
	}

	identity, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ("email"),
			authidentity.ProviderKeyEQ("email"),
			authidentity.ProviderSubjectEQ(subject),
		).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if identity.UserID != userID {
		return ErrAuthIdentityOwnershipConflict
	}
	return nil
}

func replaceEmailAuthIdentityWithClient(ctx context.Context, client *dbent.Client, userID int64, oldEmail, newEmail string, source string) error {
	newSubject := normalizeEmailAuthIdentitySubject(newEmail)
	if err := ensureEmailAuthIdentityWithClient(ctx, client, userID, newEmail, source); err != nil {
		return err
	}

	oldSubject := normalizeEmailAuthIdentitySubject(oldEmail)
	if oldSubject == "" || oldSubject == newSubject {
		return nil
	}

	_, err := clientFromContext(ctx, client).AuthIdentity.Delete().
		Where(
			authidentity.UserIDEQ(userID),
			authidentity.ProviderTypeEQ("email"),
			authidentity.ProviderKeyEQ("email"),
			authidentity.ProviderSubjectEQ(oldSubject),
		).
		Exec(ctx)
	return err
}

func normalizeEmailAuthIdentitySubject(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return ""
	}
	if strings.HasSuffix(normalized, service.LinuxDoConnectSyntheticEmailDomain) ||
		strings.HasSuffix(normalized, service.OIDCConnectSyntheticEmailDomain) ||
		strings.HasSuffix(normalized, service.WeChatConnectSyntheticEmailDomain) ||
		strings.HasSuffix(normalized, service.DingTalkConnectSyntheticEmailDomain) {
		return ""
	}
	return normalized
}

func (r *userRepository) Delete(ctx context.Context, id int64) error {
	// 复用 context 中已存在的事务（如 AdminService.DeleteUser 把删 Key 与删 User 包在同一事务中），
	// 由调用方负责提交/回滚，保证两者的原子性。
	if existingTx := dbent.TxFromContext(ctx); existingTx != nil {
		return r.deleteUser(ctx, existingTx.Client(), id)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	exec := r.client
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		exec = tx.Client()
	}
	// err == dbent.ErrTxStarted 时复用当前事务（exec = r.client）。

	if err := r.deleteUser(ctx, exec, id); err != nil {
		return err
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return translatePersistenceError(err, service.ErrUserNotFound, nil)
		}
	}
	return nil
}

// deleteUser 在给定 client（可能是外部事务 client）上删除用户及其身份关联记录，自身不开启/提交事务。
func (r *userRepository) deleteUser(ctx context.Context, exec *dbent.Client, id int64) error {
	identityIDs, err := exec.AuthIdentity.Query().
		Where(authidentity.UserIDEQ(id)).
		IDs(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	if len(identityIDs) > 0 {
		if _, err := exec.IdentityAdoptionDecision.Update().
			Where(identityadoptiondecision.IdentityIDIn(identityIDs...)).
			ClearIdentityID().
			Save(ctx); err != nil {
			return translatePersistenceError(err, service.ErrUserNotFound, nil)
		}
		if _, err := exec.AuthIdentityChannel.Delete().
			Where(authidentitychannel.IdentityIDIn(identityIDs...)).
			Exec(ctx); err != nil {
			return translatePersistenceError(err, service.ErrUserNotFound, nil)
		}
		if _, err := exec.AuthIdentity.Delete().
			Where(authidentity.UserIDEQ(id)).
			Exec(ctx); err != nil {
			return translatePersistenceError(err, service.ErrUserNotFound, nil)
		}
	}

	affected, err := exec.User.Delete().Where(dbuser.IDEQ(id)).Exec(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	if affected == 0 {
		return service.ErrUserNotFound
	}
	return nil
}

func (r *userRepository) List(ctx context.Context, params pagination.PaginationParams) ([]service.User, *pagination.PaginationResult, error) {
	return r.ListWithFilters(ctx, params, service.UserListFilters{})
}

func (r *userRepository) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters service.UserListFilters) ([]service.User, *pagination.PaginationResult, error) {
	// SkipSoftDelete 仅作用于 User 身份解析（下方 Count/All）；订阅、分组等关联实体沿用原始 ctx，避免穿透到这些同样带软删除的实体而带出已删除行。
	userCtx := ctx
	if filters.IncludeDeleted {
		userCtx = mixins.SkipSoftDelete(ctx)
	}

	q := r.client.User.Query()

	if filters.Status != "" {
		q = q.Where(dbuser.StatusEQ(filters.Status))
	}
	if filters.Role != "" {
		q = q.Where(dbuser.RoleEQ(filters.Role))
	}
	if filters.Search != "" {
		q = q.Where(
			dbuser.Or(
				dbuser.EmailContainsFold(filters.Search),
				dbuser.UsernameContainsFold(filters.Search),
				dbuser.NotesContainsFold(filters.Search),
				dbuser.HasAPIKeysWith(apikey.KeyContainsFold(filters.Search)),
			),
		)
	}

	if filters.GroupName != "" {
		q = q.Where(dbuser.HasAllowedGroupsWith(
			dbgroup.NameContainsFold(filters.GroupName),
		))
	}

	if filters.APIKeyGroupID > 0 {
		// 按"API Key 实际绑定的分组"过滤：用户只要有任意一个未软删除的 API Key
		// 绑定到该分组即命中（EXISTS 语义）。
		// 注意：SoftDeleteMixin 的拦截器不会自动下沉到 HasAPIKeysWith 子查询，
		// 必须显式加 apikey.DeletedAtIsNil()，否则已软删除的 key 会污染过滤结果。
		q = q.Where(dbuser.HasAPIKeysWith(
			apikey.GroupIDEQ(filters.APIKeyGroupID),
			apikey.DeletedAtIsNil(),
		))
	}

	// If attribute filters are specified, we need to filter by user IDs first
	var allowedUserIDs []int64
	if len(filters.Attributes) > 0 {
		var attrErr error
		allowedUserIDs, attrErr = r.filterUsersByAttributes(ctx, filters.Attributes)
		if attrErr != nil {
			return nil, nil, attrErr
		}
		if len(allowedUserIDs) == 0 {
			// No users match the attribute filters
			return []service.User{}, paginationResultFromTotal(0, params), nil
		}
		q = q.Where(dbuser.IDIn(allowedUserIDs...))
	}

	total, err := q.Clone().Count(userCtx)
	if err != nil {
		return nil, nil, err
	}

	usersQuery := q.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range userListOrder(params) {
		usersQuery = usersQuery.Order(order)
	}

	users, err := usersQuery.All(userCtx)
	if err != nil {
		return nil, nil, err
	}

	outUsers := make([]service.User, 0, len(users))
	if len(users) == 0 {
		return outUsers, paginationResultFromTotal(int64(total), params), nil
	}

	userIDs := make([]int64, 0, len(users))
	userMap := make(map[int64]*service.User, len(users))
	for i := range users {
		userIDs = append(userIDs, users[i].ID)
		u := userEntityToService(users[i])
		outUsers = append(outUsers, *u)
		userMap[u.ID] = &outUsers[len(outUsers)-1]
	}

	shouldLoadSubscriptions := filters.IncludeSubscriptions == nil || *filters.IncludeSubscriptions
	if shouldLoadSubscriptions {
		// Batch load active subscriptions with groups to avoid N+1.
		subs, err := r.client.UserSubscription.Query().
			Where(
				usersubscription.UserIDIn(userIDs...),
				usersubscription.StatusEQ(service.SubscriptionStatusActive),
			).
			WithGroup().
			All(ctx)
		if err != nil {
			return nil, nil, err
		}

		for i := range subs {
			if u, ok := userMap[subs[i].UserID]; ok {
				u.Subscriptions = append(u.Subscriptions, *userSubscriptionEntityToService(subs[i]))
			}
		}
	}

	allowedGroupsByUser, err := r.loadAllowedGroups(ctx, userIDs)
	if err != nil {
		return nil, nil, err
	}
	for id, u := range userMap {
		if groups, ok := allowedGroupsByUser[id]; ok {
			u.AllowedGroups = groups
		}
	}
	blockedGroupsByUser, err := r.loadBlockedGroups(ctx, userIDs)
	if err != nil {
		return nil, nil, err
	}
	for id, u := range userMap {
		u.BlockedGroups = blockedGroupsByUser[id]
	}

	return outUsers, paginationResultFromTotal(int64(total), params), nil
}

func userListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)

	if sortBy == "last_used_at" {
		return userLastUsedAtOrder(sortOrder)
	}

	var field string
	defaultField := true
	nullsLastField := false
	switch sortBy {
	case "email":
		field = dbuser.FieldEmail
		defaultField = false
	case "username":
		field = dbuser.FieldUsername
		defaultField = false
	case "role":
		field = dbuser.FieldRole
		defaultField = false
	case "balance":
		field = dbuser.FieldBalance
		defaultField = false
	case "concurrency":
		field = dbuser.FieldConcurrency
		defaultField = false
	case "status":
		field = dbuser.FieldStatus
		defaultField = false
	case "created_at":
		field = dbuser.FieldCreatedAt
		defaultField = false
	case "last_active_at":
		field = dbuser.FieldLastActiveAt
		defaultField = false
		nullsLastField = true
	default:
		field = dbuser.FieldID
	}

	if sortOrder == pagination.SortOrderAsc {
		if defaultField && field == dbuser.FieldID {
			return []func(*entsql.Selector){dbent.Asc(dbuser.FieldID)}
		}
		if nullsLastField {
			return []func(*entsql.Selector){
				entsql.OrderByField(field, entsql.OrderNullsLast()).ToFunc(),
				dbent.Asc(dbuser.FieldID),
			}
		}
		return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(dbuser.FieldID)}
	}
	if defaultField && field == dbuser.FieldID {
		return []func(*entsql.Selector){dbent.Desc(dbuser.FieldID)}
	}
	if nullsLastField {
		return []func(*entsql.Selector){
			entsql.OrderByField(field, entsql.OrderDesc(), entsql.OrderNullsLast()).ToFunc(),
			dbent.Desc(dbuser.FieldID),
		}
	}
	return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(dbuser.FieldID)}
}

func (r *userRepository) GetLatestUsedAtByUserIDs(ctx context.Context, userIDs []int64) (map[int64]*time.Time, error) {
	result := make(map[int64]*time.Time, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	if r.sql == nil {
		return nil, fmt.Errorf("sql executor is not configured")
	}

	const query = `
		SELECT user_id, MAX(created_at) AS last_used_at
		FROM usage_logs
		WHERE user_id = ANY($1)
		GROUP BY user_id
	`

	rows, err := r.sql.QueryContext(ctx, query, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			userID     int64
			lastUsedAt time.Time
		)
		if scanErr := rows.Scan(&userID, &lastUsedAt); scanErr != nil {
			return nil, scanErr
		}
		ts := lastUsedAt.UTC()
		result[userID] = &ts
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *userRepository) GetLatestUsedAtByUserID(ctx context.Context, userID int64) (*time.Time, error) {
	latestByUserID, err := r.GetLatestUsedAtByUserIDs(ctx, []int64{userID})
	if err != nil {
		return nil, err
	}
	return latestByUserID[userID], nil
}

func userLastUsedAtOrder(sortOrder string) []func(*entsql.Selector) {
	orderExpr := func(direction, nulls string, tieOrder func(string) string) func(*entsql.Selector) {
		return func(s *entsql.Selector) {
			subquery := fmt.Sprintf("(SELECT MAX(created_at) FROM usage_logs WHERE user_id = %s)", s.C(dbuser.FieldID))
			s.OrderExpr(entsql.Expr(subquery + " " + direction + " NULLS " + nulls))
			s.OrderBy(tieOrder(s.C(dbuser.FieldID)))
		}
	}

	if sortOrder == pagination.SortOrderAsc {
		return []func(*entsql.Selector){
			orderExpr("ASC", "FIRST", entsql.Asc),
		}
	}
	return []func(*entsql.Selector){
		orderExpr("DESC", "LAST", entsql.Desc),
	}
}

// filterUsersByAttributes returns user IDs that match ALL the given attribute filters
func (r *userRepository) filterUsersByAttributes(ctx context.Context, attrs map[int64]string) ([]int64, error) {
	if len(attrs) == 0 {
		return nil, nil
	}

	if r.sql == nil {
		return nil, fmt.Errorf("sql executor is not configured")
	}

	clauses := make([]string, 0, len(attrs))
	args := make([]any, 0, len(attrs)*2+1)
	argIndex := 1
	for attrID, value := range attrs {
		clauses = append(clauses, fmt.Sprintf("(attribute_id = $%d AND value ILIKE $%d)", argIndex, argIndex+1))
		args = append(args, attrID, "%"+value+"%")
		argIndex += 2
	}

	query := fmt.Sprintf(
		`SELECT user_id
		 FROM user_attribute_values
		 WHERE %s
		 GROUP BY user_id
		 HAVING COUNT(DISTINCT attribute_id) = $%d`,
		strings.Join(clauses, " OR "),
		argIndex,
	)
	args = append(args, len(attrs))

	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]int64, 0)
	for rows.Next() {
		var userID int64
		if scanErr := rows.Scan(&userID); scanErr != nil {
			return nil, scanErr
		}
		result = append(result, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *userRepository) UpdateBalance(ctx context.Context, id int64, amount float64) error {
	client := clientFromContext(ctx, r.client)
	update := client.User.Update().Where(dbuser.IDEQ(id)).AddBalance(amount)
	// Track cumulative recharge amount for percentage-based notifications
	if amount > 0 {
		update = update.AddTotalRecharged(amount)
	}
	n, err := update.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	if n == 0 {
		return service.ErrUserNotFound
	}
	return nil
}

// AddBalance 仅增加用户余额，不累计到 total_recharged。
// 用于签到、活动赠送等非充值来源，避免污染累计充值统计和低余额提醒基准。
func (r *userRepository) AddBalance(ctx context.Context, id int64, amount float64) error {
	client := clientFromContext(ctx, r.client)
	n, err := client.User.Update().Where(dbuser.IDEQ(id)).AddBalance(amount).Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	if n == 0 {
		return service.ErrUserNotFound
	}
	return nil
}

func (r *userRepository) ApplyRedeemBalanceAdjustment(ctx context.Context, id int64, delta float64) error {
	const updateSQL = `
		UPDATE users
		SET balance = GREATEST(balance + $1, 0), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, updateSQL, delta, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrUserNotFound
	}
	return nil
}

// DeductBalance 扣除用户余额
// 透支策略：允许余额变为负数，确保当前请求能够完成
// 中间件会阻止余额 <= 0 的用户发起后续请求
func (r *userRepository) DeductBalance(ctx context.Context, id int64, amount float64) error {
	client := clientFromContext(ctx, r.client)
	n, err := client.User.Update().
		Where(dbuser.IDEQ(id), dbuser.BalanceGTE(amount)).
		AddBalance(-amount).
		Save(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	n, err = client.User.Update().
		Where(dbuser.IDEQ(id)).
		AddBalance(-amount).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrUserNotFound
	}
	return nil
}

// DeductAvailableBalance atomically deducts min(amount, max(balance, 0)).
// Unlike DeductBalance, this refund-specific operation never increases an
// existing deficit or permits a concurrent deduction to cause an overdraft.
func (r *userRepository) DeductAvailableBalance(ctx context.Context, id int64, amount float64) (deducted float64, err error) {
	if amount < 0 {
		return 0, fmt.Errorf("deduction amount must be nonnegative")
	}
	const updateSQL = `
		WITH target AS (
			SELECT id, balance
			FROM users
			WHERE id = $2 AND deleted_at IS NULL
			FOR UPDATE
		), updated AS (
			UPDATE users AS u
			SET balance = target.balance - LEAST($1, GREATEST(target.balance, 0)), updated_at = NOW()
			FROM target
			WHERE u.id = target.id AND u.deleted_at IS NULL
			RETURNING target.balance - u.balance AS deducted
		)
		SELECT deducted FROM updated
	`
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, updateSQL, amount, id)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	if !rows.Next() {
		if rowsErr := rows.Err(); rowsErr != nil {
			return 0, rowsErr
		}
		return 0, service.ErrUserNotFound
	}
	if err := rows.Scan(&deducted); err != nil {
		return 0, err
	}
	return deducted, rows.Err()
}

// AdjustBalance 原子地把 delta 累加到余额上，结果为负时整条语句不生效。
// 相比"读余额 → 算新值 → 整行写回"，这里把读与写压进同一条 UPDATE，
// 并发的计费扣款不会被旧快照覆盖。
func (r *userRepository) AdjustBalance(ctx context.Context, id int64, delta float64) (service.BalanceChange, error) {
	const updateSQL = `
		UPDATE users
		SET balance = balance + $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance + $1 >= 0
		RETURNING balance - $1, balance
	`
	change, ok, err := scanBalanceChange(ctx, clientFromContext(ctx, r.client), updateSQL, delta, id)
	if err != nil {
		return service.BalanceChange{}, err
	}
	if ok {
		return change, nil
	}

	// 0 行既可能是用户不存在，也可能是余额不足以承受这次扣减，需要区分。
	current, err := r.currentBalance(ctx, id)
	if err != nil {
		return service.BalanceChange{}, err
	}
	return service.BalanceChange{Old: current, New: current + delta}, service.ErrBalanceNegative
}

// SetBalance 原子地把余额置为 value，并返回变更前后的值。
func (r *userRepository) SetBalance(ctx context.Context, id int64, value float64) (service.BalanceChange, error) {
	if value < 0 {
		// 连同当前余额一起返回，便于上层给出可读的错误信息。
		current, err := r.currentBalance(ctx, id)
		if err != nil {
			return service.BalanceChange{}, err
		}
		return service.BalanceChange{Old: current, New: value}, service.ErrBalanceNegative
	}
	const updateSQL = `
		UPDATE users AS u
		SET balance = $1, updated_at = NOW()
		FROM (SELECT id, balance FROM users WHERE id = $2 AND deleted_at IS NULL) AS prev
		WHERE u.id = prev.id AND u.deleted_at IS NULL
		RETURNING prev.balance, u.balance
	`
	change, ok, err := scanBalanceChange(ctx, clientFromContext(ctx, r.client), updateSQL, value, id)
	if err != nil {
		return service.BalanceChange{}, err
	}
	if !ok {
		return service.BalanceChange{}, service.ErrUserNotFound
	}
	return change, nil
}

// currentBalance 读取用户当前余额，用户不存在时返回 ErrUserNotFound。
func (r *userRepository) currentBalance(ctx context.Context, id int64) (balance float64, err error) {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx,
		`SELECT balance FROM users WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	if !rows.Next() {
		if rowsErr := rows.Err(); rowsErr != nil {
			return 0, rowsErr
		}
		return 0, service.ErrUserNotFound
	}
	if err := rows.Scan(&balance); err != nil {
		return 0, err
	}
	return balance, rows.Err()
}

// scanBalanceChange 执行一条 RETURNING 旧余额、新余额的语句。ok 为 false 表示语句未命中任何行。
func scanBalanceChange(ctx context.Context, client *dbent.Client, query string, args ...any) (change service.BalanceChange, ok bool, err error) {
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return service.BalanceChange{}, false, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	if !rows.Next() {
		if rowsErr := rows.Err(); rowsErr != nil {
			return service.BalanceChange{}, false, rowsErr
		}
		return service.BalanceChange{}, false, nil
	}
	if err := rows.Scan(&change.Old, &change.New); err != nil {
		return service.BalanceChange{}, false, err
	}
	return change, true, rows.Err()
}

func (r *userRepository) UpdateConcurrency(ctx context.Context, id int64, amount int) error {
	client := clientFromContext(ctx, r.client)
	n, err := client.User.Update().Where(dbuser.IDEQ(id)).AddConcurrency(amount).Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	if n == 0 {
		return service.ErrUserNotFound
	}
	return nil
}

func (r *userRepository) ApplyRedeemConcurrencyAdjustment(ctx context.Context, id int64, delta int) error {
	const updateSQL = `
		UPDATE users
		SET concurrency = GREATEST(concurrency + $1, 0), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, updateSQL, delta, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrUserNotFound
	}
	return nil
}

func (r *userRepository) BatchSetConcurrency(ctx context.Context, userIDs []int64, value int) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	if value < 0 {
		value = 0
	}
	res, err := r.sql.ExecContext(ctx,
		"UPDATE users SET concurrency = $1, updated_at = NOW() WHERE id = ANY($2) AND deleted_at IS NULL",
		value, pq.Array(userIDs))
	if err != nil {
		return 0, fmt.Errorf("batch set concurrency: %w", err)
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

func (r *userRepository) BatchAddConcurrency(ctx context.Context, userIDs []int64, delta int) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	res, err := r.sql.ExecContext(ctx,
		"UPDATE users SET concurrency = GREATEST(concurrency + $1, 0), updated_at = NOW() WHERE id = ANY($2) AND deleted_at IS NULL",
		delta, pq.Array(userIDs))
	if err != nil {
		return 0, fmt.Errorf("batch add concurrency: %w", err)
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

func (r *userRepository) BatchUpdateLimits(ctx context.Context, userIDs []int64, concurrency, rpmLimit *int) (int, error) {
	if len(userIDs) == 0 || (concurrency == nil && rpmLimit == nil) {
		return 0, nil
	}

	setClauses := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if concurrency != nil {
		value := max(*concurrency, 0)
		args = append(args, value)
		setClauses = append(setClauses, fmt.Sprintf("concurrency = $%d", len(args)))
	}
	if rpmLimit != nil {
		value := max(*rpmLimit, 0)
		args = append(args, value)
		setClauses = append(setClauses, fmt.Sprintf("rpm_limit = $%d", len(args)))
	}
	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, pq.Array(userIDs))

	query := fmt.Sprintf(
		"UPDATE users SET %s WHERE id = ANY($%d) AND deleted_at IS NULL",
		strings.Join(setClauses, ", "),
		len(args),
	)
	res, err := r.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("batch update user limits: %w", err)
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

func (r *userRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	return r.client.User.Query().Where(userEmailLookupPredicate(email)).Exist(ctx)
}

// emailAliasCandidateLimit 限制一次别名查重最多取回的候选行数。探针都以去点后的
// 本地部分为前缀锚定（见 dotStrippedEmailExpr），正常收件箱的变体只有个位数；
// 上限只是兜底，避免公开未鉴权的注册/发码端点把大表整张读进内存。
const emailAliasCandidateLimit = 50

// ExistsByEmailAlias 见 service.UserRepository。软删除过滤沿用 ExistsByEmail 的默认行为。
func (r *userRepository) ExistsByEmailAlias(ctx context.Context, email string) (bool, error) {
	return existsByEmailAliasWithClient(ctx, clientFromContext(ctx, r.client), email)
}

func existsByEmailAliasWithClient(ctx context.Context, client *dbent.Client, email string) (bool, error) {
	if client == nil {
		return false, nil
	}
	probes := service.EmailAliasDedupProbes(email)
	if len(probes) == 0 {
		return false, nil
	}

	preds := make([]predicate.User, 0, 2*len(probes))
	for _, probe := range probes {
		preds = append(preds,
			dotStrippedEmailEQ(probe.Local+"@"+probe.Domain),
			// "+后缀"的内容未知，只能按前缀匹配。
			dotStrippedEmailLike(escapeLikeWildcards(probe.Local)+"+%@"+escapeLikeWildcards(probe.Domain)),
		)
	}
	candidates, err := client.User.Query().
		Where(dbuser.Or(preds...)).
		Limit(emailAliasCandidateLimit).
		Select(dbuser.FieldEmail).
		Strings(ctx)
	if err != nil {
		return false, err
	}

	// 探针会有过度匹配（点号只在 Gmail 家族无意义），最终判定必须回到完整归一化规则。
	identity := service.NormalizeEmailForAliasDedup(email)
	for _, candidate := range candidates {
		if service.NormalizeEmailForAliasDedup(candidate) == identity {
			return true, nil
		}
	}
	return false, nil
}

// dotStrippedEmailExpr 渲染下面的表达式：去掉存量邮箱的大小写、首尾空白（与
// userEmailLookupPredicate 的精确匹配口径一致，历史数据存在带空白的行）以及全部点号。
//
//	REPLACE(LOWER(TRIM(email)), '.', '')
//
// 两侧都去点，因此一个域名探针即可同时覆盖 Gmail 点号变体与 FQDN 根点（user@gmail.com.）。
// migrations/190 为同一表达式建了索引。
func dotStrippedEmailExpr(b *entsql.Builder, s *entsql.Selector) *entsql.Builder {
	return b.WriteString("REPLACE(LOWER(TRIM(").
		Ident(s.C(dbuser.FieldEmail)).
		WriteString(")), '.', '')")
}

func dotStrippedEmailEQ(value string) predicate.User {
	return predicate.User(func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			dotStrippedEmailExpr(b, s).WriteString(" = ").Arg(value)
		}))
	})
}

func dotStrippedEmailLike(pattern string) predicate.User {
	return predicate.User(func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			dotStrippedEmailExpr(b, s).WriteString(" LIKE ").Arg(pattern).WriteString(` ESCAPE '\'`)
		}))
	})
}

// escapeLikeWildcards 转义 LIKE 元字符：本地部分合法可含 % 与 _，不转义会扩大匹配面。
var likeWildcardEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

func escapeLikeWildcards(value string) string {
	return likeWildcardEscaper.Replace(value)
}

func ensureNormalizedEmailAvailableWithClient(ctx context.Context, client *dbent.Client, userID int64, email string) error {
	client = clientFromContext(ctx, client)
	if client == nil {
		return nil
	}

	matches, err := client.User.Query().
		Where(userEmailLookupPredicate(email)).
		All(ctx)
	if err != nil {
		return err
	}
	for _, match := range matches {
		if match.ID != userID {
			return service.ErrEmailExists
		}
	}
	return nil
}

func userEmailLookupPredicate(email string) predicate.User {
	normalized := normalizeEmailLookupValue(email)
	if normalized == "" {
		return dbuser.EmailEQ(email)
	}
	return predicate.User(func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("LOWER(TRIM(").
				Ident(s.C(dbuser.FieldEmail)).
				WriteString(")) = ").
				Arg(normalized)
		}))
	})
}

func normalizeEmailLookupValue(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizedEmailUniquenessLockKey(email string) string {
	normalized := normalizeEmailLookupValue(email)
	if normalized == "" {
		return ""
	}
	return "users:normalized-email:" + normalized
}

func registrationEmailDomainLockKey(domain string) string {
	domain = normalizeEmailDomain(domain)
	if domain == "" {
		return ""
	}
	return "users:registration-email-domain:" + domain
}

func normalizeEmailDomain(domain string) string {
	return service.NormalizeRegistrationEmailDomain(domain)
}

func countUsersByEmailDomainWithClient(ctx context.Context, client *dbent.Client, domain string) (int, error) {
	client = clientFromContext(ctx, client)
	domain = normalizeEmailDomain(domain)
	if client == nil || domain == "" {
		return 0, nil
	}
	return client.User.Query().Where(userEmailDomainPredicate(domain)).Count(ctx)
}

func userEmailDomainPredicate(domain string) predicate.User {
	domain = normalizeEmailDomain(domain)
	escapedDomain := escapeLikeWildcards(domain)
	exactPattern := "%@" + escapedDomain
	subdomainPattern := "%@%." + escapedDomain
	return predicate.User(func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("(RTRIM(LOWER(TRIM(").
				Ident(s.C(dbuser.FieldEmail)).
				WriteString(")), '.') LIKE ").
				Arg(exactPattern).
				WriteString(` ESCAPE '\' OR RTRIM(LOWER(TRIM(`).
				Ident(s.C(dbuser.FieldEmail)).
				WriteString(")), '.') LIKE ").
				Arg(subdomainPattern).
				WriteString(` ESCAPE '\'`).
				WriteString(")")
		}))
	})
}

// emailAliasUniquenessLockKey 按收件箱身份（而非邮箱字面量）加锁，使同一收件箱的不同
// 别名变体在注册时互斥。
func emailAliasUniquenessLockKey(email string) string {
	identity := service.NormalizeEmailForAliasDedup(email)
	if identity == "" {
		return ""
	}
	return "users:email-alias-identity:" + identity
}

func (r *userRepository) AddGroupToAllowedGroups(ctx context.Context, userID int64, groupID int64) error {
	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return fmt.Errorf("sql executor is not configured")
	}
	_, err := exec.ExecContext(ctx, `
INSERT INTO user_allowed_groups (user_id, group_id, created_at, updated_at, source, expires_at, source_order_id, notes)
VALUES ($1, $2, NOW(), NOW(), $3, NULL, NULL, '')
ON CONFLICT (user_id, group_id) DO UPDATE SET
    source = $3,
    expires_at = NULL,
    source_order_id = NULL,
    updated_at = NOW()
`, userID, groupID, service.UserAllowedGroupSourceManual)
	if isSQLNoRowsError(err) {
		return nil
	}
	return err
}

func (r *userRepository) GrantTemporaryAllowedGroup(ctx context.Context, input service.TemporaryAllowedGroupGrantInput) (*service.TemporaryAllowedGroupGrantResult, error) {
	if input.UserID <= 0 || input.GroupID <= 0 || input.ValidityDays <= 0 {
		return nil, fmt.Errorf("invalid temporary allowed group grant input")
	}
	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return nil, fmt.Errorf("sql executor is not configured")
	}

	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = service.UserAllowedGroupSourceAffiliatePaymentReward
	}
	var sourceOrderID any
	if input.SourceOrderID != nil {
		sourceOrderID = *input.SourceOrderID
	}

	rows, err := exec.QueryContext(ctx, `
INSERT INTO user_allowed_groups (user_id, group_id, created_at, updated_at, source, source_order_id, notes, expires_at)
VALUES (
    $1,
    $2,
    $4,
    $4,
    $5,
    $6,
    $7,
    LEAST($4::timestamptz + make_interval(days => $3::int), $8::timestamptz)
)
ON CONFLICT (user_id, group_id) DO UPDATE SET
    updated_at = $4,
    source = CASE
        WHEN user_allowed_groups.expires_at IS NULL THEN user_allowed_groups.source
        ELSE EXCLUDED.source
    END,
    source_order_id = CASE
        WHEN user_allowed_groups.expires_at IS NULL THEN user_allowed_groups.source_order_id
        ELSE EXCLUDED.source_order_id
    END,
    notes = CASE
        WHEN user_allowed_groups.expires_at IS NULL THEN user_allowed_groups.notes
        WHEN COALESCE(EXCLUDED.notes, '') = '' THEN user_allowed_groups.notes
        WHEN COALESCE(user_allowed_groups.notes, '') = '' THEN EXCLUDED.notes
        ELSE user_allowed_groups.notes || E'\n' || EXCLUDED.notes
    END,
    expires_at = CASE
        WHEN user_allowed_groups.expires_at IS NULL THEN NULL
        WHEN user_allowed_groups.expires_at > $4::timestamptz THEN LEAST(user_allowed_groups.expires_at + make_interval(days => $3::int), $8::timestamptz)
        ELSE LEAST($4::timestamptz + make_interval(days => $3::int), $8::timestamptz)
    END
RETURNING expires_at
`, input.UserID, input.GroupID, input.ValidityDays, now, source, sourceOrderID, input.Notes, service.MaxExpiresAt)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}
	var expiresAt sql.NullTime
	if err := rows.Scan(&expiresAt); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := &service.TemporaryAllowedGroupGrantResult{
		UserID:    input.UserID,
		GroupID:   input.GroupID,
		Permanent: !expiresAt.Valid,
	}
	if expiresAt.Valid {
		result.ExpiresAt = &expiresAt.Time
	}
	return result, nil
}

func (r *userRepository) ListActiveUserGroupAccessMeta(ctx context.Context, userID int64) (map[int64]service.UserGroupAccessMeta, error) {
	if userID <= 0 {
		return map[int64]service.UserGroupAccessMeta{}, nil
	}
	byUser, err := r.ListActiveUserGroupAccessMetaByUserIDs(ctx, []int64{userID})
	if err != nil {
		return nil, err
	}
	if out, ok := byUser[userID]; ok {
		return out, nil
	}
	return map[int64]service.UserGroupAccessMeta{}, nil
}

func (r *userRepository) ListActiveUserGroupAccessMetaByUserIDs(ctx context.Context, userIDs []int64) (map[int64]map[int64]service.UserGroupAccessMeta, error) {
	out := make(map[int64]map[int64]service.UserGroupAccessMeta, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	uniqueUserIDs := uniquePositiveInt64s(userIDs)
	if len(uniqueUserIDs) == 0 {
		return out, nil
	}
	rows, err := clientFromContext(ctx, r.client).UserAllowedGroup.Query().
		Where(
			userallowedgroup.UserIDIn(uniqueUserIDs...),
			userallowedgroup.Or(
				userallowedgroup.ExpiresAtIsNil(),
				userallowedgroup.ExpiresAtGT(time.Now().UTC()),
			),
		).
		Order(userallowedgroup.ByUserID(), userallowedgroup.ByGroupID()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	for i := range rows {
		item := rows[i]
		if out[item.UserID] == nil {
			out[item.UserID] = make(map[int64]service.UserGroupAccessMeta)
		}
		out[item.UserID][item.GroupID] = userAllowedGroupEntityToAccessMeta(item)
	}
	return out, nil
}

func (r *userRepository) ListActiveUserGroupAccessMetaByGroupID(ctx context.Context, groupID int64, page, pageSize int) ([]service.UserGroupAccessMeta, int64, error) {
	if groupID <= 0 {
		return []service.UserGroupAccessMeta{}, 0, nil
	}
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	q := clientFromContext(ctx, r.client).UserAllowedGroup.Query().
		Where(
			userallowedgroup.GroupIDEQ(groupID),
			userallowedgroup.Or(
				userallowedgroup.ExpiresAtIsNil(),
				userallowedgroup.ExpiresAtGT(time.Now().UTC()),
			),
		)

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := q.
		WithUser(func(uq *dbent.UserQuery) {
			uq.Select(dbuser.FieldID, dbuser.FieldEmail, dbuser.FieldUsername, dbuser.FieldStatus)
		}).
		Order(userallowedgroup.ByExpiresAt(entsql.OrderNullsLast()), userallowedgroup.ByUserID()).
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]service.UserGroupAccessMeta, 0, len(rows))
	for i := range rows {
		meta := userAllowedGroupEntityToAccessMeta(rows[i])
		if rows[i].Edges.User != nil {
			meta.UserEmail = rows[i].Edges.User.Email
			meta.Username = rows[i].Edges.User.Username
			meta.UserStatus = rows[i].Edges.User.Status
		}
		out = append(out, meta)
	}
	return out, int64(total), nil
}

func (r *userRepository) ListAuthorizedUsersByGroup(ctx context.Context, groupID int64, params pagination.PaginationParams, search string) ([]service.GroupAuthorizedUser, *pagination.PaginationResult, error) {
	if groupID <= 0 {
		return []service.GroupAuthorizedUser{}, paginationResultFromTotal(0, params), nil
	}

	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return nil, nil, fmt.Errorf("sql executor is not configured")
	}

	clauses := []string{
		"uag.group_id = $1",
		"u.deleted_at IS NULL",
		"(uag.expires_at IS NULL OR uag.expires_at > NOW())",
	}
	args := []any{groupID}
	search = strings.TrimSpace(search)
	if search != "" {
		args = append(args, "%"+search+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		clauses = append(clauses, fmt.Sprintf("(u.email ILIKE %s OR u.username ILIKE %s OR u.notes ILIKE %s OR uag.notes ILIKE %s)", placeholder, placeholder, placeholder, placeholder))
	}
	whereSQL := strings.Join(clauses, " AND ")

	var total int64
	countQuery := fmt.Sprintf(`
SELECT COUNT(*)
FROM user_allowed_groups uag
JOIN users u ON u.id = uag.user_id
WHERE %s
`, whereSQL)
	if err := scanSingleRow(ctx, exec, countQuery, args, &total); err != nil {
		return nil, nil, err
	}
	if total == 0 {
		return []service.GroupAuthorizedUser{}, paginationResultFromTotal(0, params), nil
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, params.Limit(), params.Offset())
	limitPlaceholder := fmt.Sprintf("$%d", len(queryArgs)-1)
	offsetPlaceholder := fmt.Sprintf("$%d", len(queryArgs))
	query := fmt.Sprintf(`
SELECT
    u.id,
    u.email,
    u.username,
    u.notes,
    u.status,
    u.role,
    u.balance,
    u.concurrency,
    u.rpm_limit,
    uag.source,
    uag.source_order_id,
    uag.expires_at,
    uag.created_at,
    uag.updated_at
FROM user_allowed_groups uag
JOIN users u ON u.id = uag.user_id
WHERE %s
ORDER BY uag.updated_at DESC, u.id ASC
LIMIT %s OFFSET %s
`, whereSQL, limitPlaceholder, offsetPlaceholder)

	rows, err := exec.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.GroupAuthorizedUser, 0, params.Limit())
	for rows.Next() {
		var (
			item          service.GroupAuthorizedUser
			sourceOrderID sql.NullInt64
			expiresAt     sql.NullTime
		)
		if err := rows.Scan(
			&item.UserID,
			&item.Email,
			&item.Username,
			&item.Notes,
			&item.Status,
			&item.Role,
			&item.Balance,
			&item.Concurrency,
			&item.RPMLimit,
			&item.Source,
			&sourceOrderID,
			&expiresAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, nil, err
		}
		if sourceOrderID.Valid {
			value := sourceOrderID.Int64
			item.SourceOrderID = &value
		}
		if expiresAt.Valid {
			value := expiresAt.Time
			item.ExpiresAt = &value
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, paginationResultFromTotal(total, params), nil
}

func (r *userRepository) ExpireTemporaryAllowedGroups(ctx context.Context, input service.ExpireTemporaryAllowedGroupsInput) ([]service.ExpiredTemporaryAllowedGroupResult, error) {
	if input.ReplacementGroupID <= 0 {
		return nil, fmt.Errorf("replacement group id is required")
	}
	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return nil, fmt.Errorf("sql executor is not configured")
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = service.UserAllowedGroupSourceAffiliatePaymentReward
	}
	limit := input.Limit
	if limit <= 0 || limit > 1000 {
		limit = 500
	}

	rows, err := exec.QueryContext(ctx, `
WITH expired AS (
    SELECT user_id, group_id
    FROM user_allowed_groups
    WHERE source = $1
      AND expires_at IS NOT NULL
      AND expires_at <= $2
      AND group_id <> $3
    ORDER BY expires_at ASC
    LIMIT $4
    FOR UPDATE SKIP LOCKED
),
migrated AS (
    UPDATE api_keys k
    SET group_id = $3,
        updated_at = $2
    FROM expired e
    WHERE k.user_id = e.user_id
      AND k.group_id = e.group_id
      AND k.deleted_at IS NULL
    RETURNING e.user_id, e.group_id, k.id
),
deleted AS (
    DELETE FROM user_allowed_groups uag
    USING expired e
    WHERE uag.user_id = e.user_id
      AND uag.group_id = e.group_id
    RETURNING e.user_id, e.group_id
)
SELECT d.user_id, d.group_id, COUNT(m.id)::bigint AS migrated_keys
FROM deleted d
LEFT JOIN migrated m ON m.user_id = d.user_id AND m.group_id = d.group_id
GROUP BY d.user_id, d.group_id
ORDER BY d.user_id, d.group_id
`, source, now, input.ReplacementGroupID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.ExpiredTemporaryAllowedGroupResult, 0)
	for rows.Next() {
		var item service.ExpiredTemporaryAllowedGroupResult
		if err := rows.Scan(&item.UserID, &item.GroupID, &item.MigratedKeys); err != nil {
			return nil, err
		}
		item.ReplacementGroupID = input.ReplacementGroupID
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userRepository) RemoveGroupFromAllowedGroups(ctx context.Context, groupID int64) (int64, error) {
	// 仅操作 user_allowed_groups 联接表，legacy users.allowed_groups 列已弃用。
	affected, err := r.client.UserAllowedGroup.Delete().
		Where(userallowedgroup.GroupIDEQ(groupID)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	return int64(affected), nil
}

// RemoveGroupFromUserAllowedGroups 移除单个用户的指定分组权限
func (r *userRepository) RemoveGroupFromUserAllowedGroups(ctx context.Context, userID int64, groupID int64) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserAllowedGroup.Delete().
		Where(userallowedgroup.UserIDEQ(userID), userallowedgroup.GroupIDEQ(groupID)).
		Exec(ctx)
	return err
}

func (r *userRepository) GetFirstAdmin(ctx context.Context) (*service.User, error) {
	m, err := r.client.User.Query().
		Where(
			dbuser.RoleEQ(service.RoleAdmin),
			dbuser.StatusEQ(service.StatusActive),
		).
		Order(dbent.Asc(dbuser.FieldID)).
		First(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrUserNotFound, nil)
	}

	out := userEntityToService(m)
	groups, err := r.loadAllowedGroups(ctx, []int64{m.ID})
	if err != nil {
		return nil, err
	}
	if v, ok := groups[m.ID]; ok {
		out.AllowedGroups = v
	}
	blockedGroups, err := r.loadBlockedGroups(ctx, []int64{m.ID})
	if err != nil {
		return nil, err
	}
	out.BlockedGroups = blockedGroups[m.ID]
	return out, nil
}

func (r *userRepository) loadAllowedGroups(ctx context.Context, userIDs []int64) (map[int64][]int64, error) {
	out := make(map[int64][]int64, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}

	rows, err := clientFromContext(ctx, r.client).UserAllowedGroup.Query().
		Where(
			userallowedgroup.UserIDIn(userIDs...),
			userallowedgroup.Or(
				userallowedgroup.ExpiresAtIsNil(),
				userallowedgroup.ExpiresAtGT(time.Now().UTC()),
			),
		).
		Order(userallowedgroup.ByUserID(), userallowedgroup.ByGroupID()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	for i := range rows {
		out[rows[i].UserID] = append(out[rows[i].UserID], rows[i].GroupID)
	}

	return out, nil
}

func (r *userRepository) loadBlockedGroups(ctx context.Context, userIDs []int64) (map[int64][]int64, error) {
	out := make(map[int64][]int64, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}

	rows, err := clientFromContext(ctx, r.client).UserBlockedGroup.Query().
		Where(userblockedgroup.UserIDIn(userIDs...)).
		Order(userblockedgroup.ByUserID(), userblockedgroup.ByGroupID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].UserID] = append(out[rows[i].UserID], rows[i].GroupID)
	}
	return out, nil
}

func userAllowedGroupEntityToAccessMeta(item *dbent.UserAllowedGroup) service.UserGroupAccessMeta {
	if item == nil {
		return service.UserGroupAccessMeta{}
	}
	return service.UserGroupAccessMeta{
		UserID:        item.UserID,
		GroupID:       item.GroupID,
		Source:        item.Source,
		SourceOrderID: item.SourceOrderID,
		Notes:         item.Notes,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
		ExpiresAt:     item.ExpiresAt,
		Permanent:     item.ExpiresAt == nil,
	}
}

func uniquePositiveInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, id := range values {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func clampUserAllowedGroupExpiresAt(expiresAt *time.Time) *time.Time {
	if expiresAt == nil {
		return nil
	}
	t := expiresAt.UTC()
	if t.After(service.MaxExpiresAt) {
		t = service.MaxExpiresAt
	}
	return &t
}

func normalizeUserAllowedGroupSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return service.UserAllowedGroupSourceManual
	}
	return source
}

func (r *userRepository) SyncUserAllowedGroupAccess(ctx context.Context, userID int64, entries []service.UserAllowedGroupAccessInput) error {
	if userID <= 0 {
		return fmt.Errorf("invalid user id")
	}
	return r.syncUserAllowedGroupAccessWithClient(ctx, clientFromContext(ctx, r.client), userID, entries)
}

// syncUserAllowedGroupsWithClient 在 ent client/事务内同步用户允许分组：
// 仅操作 user_allowed_groups 联接表，legacy users.allowed_groups 列已弃用。
// 兼容旧请求：只改授权集合，保留仍被选中分组的 expires_at/source 等元数据。
func (r *userRepository) syncUserAllowedGroupsWithClient(ctx context.Context, client *dbent.Client, userID int64, groupIDs []int64) error {
	entries := make([]service.UserAllowedGroupAccessInput, 0, len(groupIDs))
	for _, groupID := range uniquePositiveInt64s(groupIDs) {
		entries = append(entries, service.UserAllowedGroupAccessInput{GroupID: groupID})
	}
	return r.syncUserAllowedGroupAccessWithClient(ctx, client, userID, entries)
}

// syncUserBlockedGroupsWithClient 在同一用户更新事务中同步公开分组黑名单。
func (r *userRepository) syncUserBlockedGroupsWithClient(ctx context.Context, client *dbent.Client, userID int64, groupIDs []int64) error {
	if client == nil {
		return nil
	}
	groupIDs = uniquePositiveInt64s(groupIDs)

	deleteQ := client.UserBlockedGroup.Delete().Where(userblockedgroup.UserIDEQ(userID))
	if len(groupIDs) > 0 {
		deleteQ = deleteQ.Where(userblockedgroup.GroupIDNotIn(groupIDs...))
	}
	if _, err := deleteQ.Exec(ctx); err != nil {
		return err
	}
	if len(groupIDs) == 0 {
		return nil
	}

	creates := make([]*dbent.UserBlockedGroupCreate, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		creates = append(creates, client.UserBlockedGroup.Create().SetUserID(userID).SetGroupID(groupID))
	}
	if err := client.UserBlockedGroup.
		CreateBulk(creates...).
		OnConflictColumns(userblockedgroup.FieldUserID, userblockedgroup.FieldGroupID).
		DoNothing().
		Exec(ctx); err != nil && !isSQLNoRowsError(err) {
		return err
	}
	return nil
}

func (r *userRepository) syncUserAllowedGroupAccessWithClient(ctx context.Context, client *dbent.Client, userID int64, entries []service.UserAllowedGroupAccessInput) error {
	if client == nil {
		return nil
	}

	now := time.Now().UTC()
	unique := make(map[int64]service.UserAllowedGroupAccessInput, len(entries))
	for _, entry := range entries {
		if entry.GroupID <= 0 {
			continue
		}
		if entry.ExpiresAtSet && entry.ExpiresAt != nil {
			expiresAt := clampUserAllowedGroupExpiresAt(entry.ExpiresAt)
			if !expiresAt.After(now) {
				return fmt.Errorf("allowed group %d expires_at must be in the future", entry.GroupID)
			}
			entry.ExpiresAt = expiresAt
		}
		entry.Source = normalizeUserAllowedGroupSource(entry.Source)
		unique[entry.GroupID] = entry
	}

	keepIDs := make([]int64, 0, len(unique))
	for groupID := range unique {
		keepIDs = append(keepIDs, groupID)
	}

	deleteQ := client.UserAllowedGroup.Delete().Where(userallowedgroup.UserIDEQ(userID))
	if len(keepIDs) > 0 {
		deleteQ = deleteQ.Where(userallowedgroup.GroupIDNotIn(keepIDs...))
	}
	if _, err := deleteQ.Exec(ctx); err != nil {
		return err
	}

	if len(unique) > 0 {
		creates := make([]*dbent.UserAllowedGroupCreate, 0, len(unique))
		for groupID, entry := range unique {
			create := client.UserAllowedGroup.Create().
				SetUserID(userID).
				SetGroupID(groupID).
				SetSource(entry.Source).
				SetNotes(entry.Notes)
			if entry.ExpiresAtSet {
				create.SetNillableExpiresAt(entry.ExpiresAt)
			}
			creates = append(creates, create)
		}
		if err := client.UserAllowedGroup.
			CreateBulk(creates...).
			OnConflictColumns(userallowedgroup.FieldUserID, userallowedgroup.FieldGroupID).
			DoNothing().
			Exec(ctx); err != nil {
			if isSQLNoRowsError(err) {
				return nil
			}
			return err
		}
	}

	for groupID, entry := range unique {
		if !entry.ExpiresAtSet && strings.TrimSpace(entry.Source) == service.UserAllowedGroupSourceManual && strings.TrimSpace(entry.Notes) == "" {
			continue
		}
		update := client.UserAllowedGroup.Update().
			Where(userallowedgroup.UserIDEQ(userID), userallowedgroup.GroupIDEQ(groupID)).
			SetSource(entry.Source).
			SetNotes(entry.Notes)
		if entry.ExpiresAtSet {
			if entry.ExpiresAt != nil {
				update.SetExpiresAt(*entry.ExpiresAt)
			} else {
				update.ClearExpiresAt()
			}
			if entry.Source == service.UserAllowedGroupSourceManual {
				update.ClearSourceOrderID()
			}
		}
		if _, err := update.Save(ctx); err != nil {
			return err
		}
	}

	return nil
}

func applyUserEntityToService(dst *service.User, src *dbent.User) {
	if dst == nil || src == nil {
		return
	}
	dst.ID = src.ID
	dst.SignupSource = src.SignupSource
	dst.LastLoginAt = src.LastLoginAt
	dst.LastActiveAt = src.LastActiveAt
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}

func userSignupSourceOrDefault(signupSource string) string {
	switch strings.TrimSpace(strings.ToLower(signupSource)) {
	case "", "email":
		return "email"
	case "linuxdo", "wechat", "oidc", "dingtalk":
		return strings.TrimSpace(strings.ToLower(signupSource))
	default:
		return "email"
	}
}

// marshalExtraEmails serializes notify email entries to JSON for storage.
func marshalExtraEmails(entries []service.NotifyEmailEntry) string {
	return service.MarshalNotifyEmails(entries)
}

// UpdateTotpSecret 更新用户的 TOTP 加密密钥
func (r *userRepository) UpdateTotpSecret(ctx context.Context, userID int64, encryptedSecret *string) error {
	client := clientFromContext(ctx, r.client)
	update := client.User.UpdateOneID(userID)
	if encryptedSecret == nil {
		update = update.ClearTotpSecretEncrypted()
	} else {
		update = update.SetTotpSecretEncrypted(*encryptedSecret)
	}
	_, err := update.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	return nil
}

// EnableTotp 启用用户的 TOTP 双因素认证
func (r *userRepository) EnableTotp(ctx context.Context, userID int64) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.User.UpdateOneID(userID).
		SetTotpEnabled(true).
		SetTotpEnabledAt(time.Now()).
		Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	return nil
}

// DisableTotp 禁用用户的 TOTP 双因素认证
func (r *userRepository) DisableTotp(ctx context.Context, userID int64) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.User.UpdateOneID(userID).
		SetTotpEnabled(false).
		ClearTotpEnabledAt().
		ClearTotpSecretEncrypted().
		Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserNotFound, nil)
	}
	return nil
}
