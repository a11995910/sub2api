.PHONY: build build-backend build-backend-embed build-frontend build-deploy build-datamanagementd test test-backend test-frontend test-frontend-critical test-datamanagementd secret-scan

FRONTEND_CRITICAL_VITEST := \
	src/views/auth/__tests__/LinuxDoCallbackView.spec.ts \
	src/views/auth/__tests__/WechatCallbackView.spec.ts \
	src/views/user/__tests__/PaymentView.spec.ts \
	src/views/user/__tests__/PaymentResultView.spec.ts \
	src/components/user/profile/__tests__/ProfileInfoCard.spec.ts \
	src/views/admin/__tests__/SettingsView.spec.ts

# 一键编译前后端
build: build-backend build-frontend

# 编译后端（复用 backend/Makefile）
build-backend:
	@$(MAKE) -C backend build

# 编译带前端资源的后端二进制（源码部署使用）
build-backend-embed:
	@$(MAKE) -C backend build-embed

# 编译前端（需要已安装依赖）
build-frontend:
	@cd frontend && NODE_OPTIONS=--max-old-space-size=1536 pnpm run build

# 源码部署编译：先生成前端 dist，再把前端资源嵌入后端二进制
build-deploy: build-frontend build-backend-embed

# 运行测试（后端 + 前端）
test: test-backend test-frontend

test-backend:
	@$(MAKE) -C backend test

test-frontend:
	@cd frontend && pnpm run lint:check
	@cd frontend && pnpm run typecheck
	@$(MAKE) test-frontend-critical

test-frontend-critical:
	@cd frontend && pnpm exec vitest run $(FRONTEND_CRITICAL_VITEST)

test-datamanagementd:
	@cd datamanagement && go test ./...

secret-scan:
	@python3 tools/secret_scan.py
