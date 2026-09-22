.PHONY: check test test-integration react

check: test react

test:
	go test ./...

test-integration:
	go test -tags=integration ./examples/customer-onboarding -count=1 -v

react:
	cd sdk/react && npm ci && npm test && npm run build
