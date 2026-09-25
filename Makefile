.PHONY: dev build up down fmt-check lint typecheck test-unit test-integration test-e2e verify load-test demo-reset
dev build up down fmt-check lint typecheck test-unit test-integration test-e2e verify load-test demo-reset:
	node scripts/task.mjs $@
